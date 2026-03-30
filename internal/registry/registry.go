// Package registry queries the npm registry for package metadata.
package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jee4nc/packwatch/internal/npmrc"
	"github.com/jee4nc/packwatch/internal/semver"
)

const (
	concurrency = 20
	maxRetries  = 3
	timeout     = 15 * time.Second
)

// PackageVersions holds version info fetched from the registry.
type PackageVersions struct {
	Name     string
	Latest   semver.Version
	Versions []semver.Version  // all published versions, sorted ascending
	Engines  map[string]string // version string → node engines constraint
	Registry string            // registry URL this was fetched from
	Error    error
}

// registryResponse is the abbreviated metadata response.
type registryResponse struct {
	DistTags map[string]string          `json:"dist-tags"`
	Versions map[string]registryVersion `json:"versions"`
}

type registryVersion struct {
	Version string          `json:"version"`
	Engines json.RawMessage `json:"engines"`
}

// parseNodeEngine extracts the "node" engine constraint from a raw engines field.
// Handles both object form {"node": ">=18"} and malformed data gracefully.
func parseNodeEngine(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try object form: {"node": ">=18"}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) == nil {
		if node, ok := obj["node"]; ok {
			if s, ok := node.(string); ok {
				return s
			}
		}
	}
	return ""
}

// ProgressFunc is called with (completed, total) during fetching.
type ProgressFunc func(completed, total int)

type fetchJob struct {
	idx  int
	name string
}

// Fetch queries the npm registry for multiple packages concurrently.
// It uses a fixed worker pool to limit goroutine count and retries
// transient failures up to maxRetries times with exponential backoff.
func Fetch(names []string, npmrcCfg npmrc.Config, onProgress ProgressFunc) []PackageVersions {
	results := make([]PackageVersions, len(names))
	var completed int64

	workers := concurrency
	if len(names) < workers {
		workers = len(names)
	}

	jobs := make(chan fetchJob, len(names))
	var wg sync.WaitGroup

	client := &http.Client{Timeout: timeout}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				registryURL := npmrcCfg.RegistryFor(job.name)
				authToken := npmrcCfg.AuthTokenFor(registryURL)
				results[job.idx] = fetchWithRetry(client, job.name, registryURL, authToken)
				c := int(atomic.AddInt64(&completed, 1))
				if onProgress != nil {
					onProgress(c, len(names))
				}
			}
		}()
	}

	for i, name := range names {
		jobs <- fetchJob{idx: i, name: name}
	}
	close(jobs)

	wg.Wait()
	return results
}

func fetchWithRetry(client *http.Client, name, registryURL, authToken string) PackageVersions {
	var result PackageVersions
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result = fetchOne(client, name, registryURL, authToken)
		if result.Error == nil {
			return result
		}
		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
			time.Sleep(backoff)
		}
	}
	return result
}

func fetchOne(client *http.Client, name, registryURL, authToken string) PackageVersions {
	result := PackageVersions{Name: name, Engines: make(map[string]string), Registry: registryURL}

	url := fmt.Sprintf("%s/%s", registryURL, name)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Error = err
		return result
	}
	// Only use abbreviated metadata for the public npm registry.
	// Private registries (JFrog, Artifactory, etc.) may not support it.
	if registryURL == "https://registry.npmjs.org" {
		req.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("request failed: %w", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		result.Error = fmt.Errorf("HTTP %d for %s", resp.StatusCode, name)
		return result
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("read body failed: %w", err)
		return result
	}

	var data registryResponse
	if err := json.Unmarshal(body, &data); err != nil {
		result.Error = fmt.Errorf("JSON decode failed: %w", err)
		return result
	}

	// Parse latest from dist-tags
	if latestStr, ok := data.DistTags["latest"]; ok {
		v, err := semver.Parse(latestStr)
		if err == nil {
			result.Latest = v
		}
	}

	// Parse all versions and their engines
	for verStr, verData := range data.Versions {
		v, err := semver.Parse(verStr)
		if err != nil {
			continue
		}
		// Skip prerelease versions
		if v.Prerelease != "" {
			continue
		}
		result.Versions = append(result.Versions, v)
		if nodeEngine := parseNodeEngine(verData.Engines); nodeEngine != "" {
			result.Engines[v.String()] = nodeEngine
		}
	}

	// Sort versions ascending
	sortVersions(result.Versions)

	return result
}

func sortVersions(versions []semver.Version) {
	// Simple insertion sort - fine for typical package version counts
	for i := 1; i < len(versions); i++ {
		key := versions[i]
		j := i - 1
		for j >= 0 && versions[j].Compare(key) > 0 {
			versions[j+1] = versions[j]
			j--
		}
		versions[j+1] = key
	}
}

// FindCompatibleLatest finds the latest version that is compatible with the given Node version.
// It walks versions from newest to oldest, checking the engines.node constraint.
func FindCompatibleLatest(pv PackageVersions, nodeVersion semver.Version) (semver.Version, bool) {
	for i := len(pv.Versions) - 1; i >= 0; i-- {
		v := pv.Versions[i]
		constraint, hasConstraint := pv.Engines[v.String()]
		if !hasConstraint {
			// No engine constraint means it's compatible
			return v, true
		}
		if semver.SatisfiesConstraints(nodeVersion, constraint) {
			return v, true
		}
	}
	return semver.Version{}, false
}
