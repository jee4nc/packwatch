// Package security queries the OSV (Open Source Vulnerabilities) database
// for known vulnerabilities in npm packages.
package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	osvBatchURL = "https://api.osv.dev/v1/querybatch"
	osvVulnURL  = "https://api.osv.dev/v1/vulns"
	batchSize   = 1000
	concurrency = 10
	timeout     = 20 * time.Second
	maxRetries  = 2
)

// Vulnerability represents a single known vulnerability.
type Vulnerability struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
	URL      string `json:"url,omitempty"`
	Fixed    bool   `json:"fixed,omitempty"`
}

// PackageResult holds vulnerability data for one package.
type PackageResult struct {
	Name            string
	Version         string
	Vulnerabilities []Vulnerability
	HighestSeverity string
	FixedByUpdate   int
	Error           error
}

// ProgressFunc is called with (completed, total) during checks.
type ProgressFunc func(completed, total int)

// Query represents a package+version to check.
type Query struct {
	Name             string
	InstalledVersion string
	AvailableVersion string // if set, also checks whether updating resolves vulns
}

// osvBatchRequest is the request body for the OSV querybatch API.
type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvQuery struct {
	Version string     `json:"version"`
	Package osvPackage `json:"package"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvBatchResponse struct {
	Results []osvBatchResult `json:"results"`
}

type osvBatchResult struct {
	Vulns []osvVulnRef `json:"vulns"`
}

type osvVulnRef struct {
	ID       string `json:"id"`
	Modified string `json:"modified"`
}

type osvVulnDetail struct {
	ID               string                 `json:"id"`
	Summary          string                 `json:"summary"`
	Severity         []osvSeverity          `json:"severity"`
	DatabaseSpecific map[string]interface{} `json:"database_specific"`
	References       []osvReference         `json:"references"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

var severityRank = map[string]int{
	"CRITICAL": 4,
	"HIGH":     3,
	"MODERATE": 2,
	"MEDIUM":   2,
	"LOW":      1,
}

// Check queries the OSV database for vulnerabilities across all given packages.
// It checks the installed version for each package. When AvailableVersion is set,
// it also checks whether updating resolves each vulnerability (one extra HTTP POST).
func Check(packages []Query, onProgress ProgressFunc) []PackageResult {
	results := make([]PackageResult, len(packages))
	for i, q := range packages {
		results[i] = PackageResult{Name: q.Name, Version: q.InstalledVersion}
	}

	client := &http.Client{Timeout: timeout}

	// Phase 1: batch query installed versions to get vuln IDs
	installedQueries := make([]batchEntry, len(packages))
	for i, q := range packages {
		installedQueries[i] = batchEntry{Name: q.Name, Version: q.InstalledVersion}
	}
	allVulnIDs := batchQueryAll(client, installedQueries)
	for i, ids := range allVulnIDs {
		if ids.err != nil {
			results[i].Error = ids.err
		}
	}

	// Collect unique vuln IDs
	uniqueIDs := make(map[string]bool)
	for _, ids := range allVulnIDs {
		for _, id := range ids.ids {
			uniqueIDs[id] = true
		}
	}

	if len(uniqueIDs) == 0 {
		if onProgress != nil {
			onProgress(1, 1)
		}
		return results
	}

	// Phase 2: fetch full details for each unique vulnerability (worker pool)
	vulnDetails := fetchVulnDetails(client, uniqueIDs, onProgress)

	// Phase 3: batch query available versions to determine resolution
	availableVulnIDs := make(map[int]map[string]bool)
	var availableQueries []batchEntry
	var availableIdx []int
	for i, q := range packages {
		if q.AvailableVersion != "" && q.AvailableVersion != q.InstalledVersion && len(allVulnIDs[i].ids) > 0 {
			availableQueries = append(availableQueries, batchEntry{Name: q.Name, Version: q.AvailableVersion})
			availableIdx = append(availableIdx, i)
		}
	}
	if len(availableQueries) > 0 {
		availResults := batchQueryAll(client, availableQueries)
		for j, ar := range availResults {
			idx := availableIdx[j]
			set := make(map[string]bool)
			for _, id := range ar.ids {
				set[id] = true
			}
			availableVulnIDs[idx] = set
		}
	}

	// Phase 4: map vulnerability details back to packages, mark resolved
	for i, ids := range allVulnIDs {
		availSet := availableVulnIDs[i]
		for _, id := range ids.ids {
			detail, ok := vulnDetails[id]
			if !ok {
				continue
			}
			sev := extractSeverity(detail)
			advisory := extractAdvisoryURL(detail)
			fixed := availSet != nil && !availSet[id]
			results[i].Vulnerabilities = append(results[i].Vulnerabilities, Vulnerability{
				ID:       detail.ID,
				Summary:  detail.Summary,
				Severity: sev,
				URL:      advisory,
				Fixed:    fixed,
			})
			if fixed {
				results[i].FixedByUpdate++
			}
			if severityRank[sev] > severityRank[results[i].HighestSeverity] {
				results[i].HighestSeverity = sev
			}
		}
		// Sort: highest severity first
		sortVulns(results[i].Vulnerabilities)
	}

	return results
}

func sortVulns(vulns []Vulnerability) {
	for i := 1; i < len(vulns); i++ {
		key := vulns[i]
		j := i - 1
		for j >= 0 && severityRank[vulns[j].Severity] < severityRank[key.Severity] {
			vulns[j+1] = vulns[j]
			j--
		}
		vulns[j+1] = key
	}
}

type batchEntry struct {
	Name    string
	Version string
}

type batchResult struct {
	ids []string
	err error
}

func batchQueryAll(client *http.Client, entries []batchEntry) []batchResult {
	results := make([]batchResult, len(entries))
	for start := 0; start < len(entries); start += batchSize {
		end := start + batchSize
		if end > len(entries) {
			end = len(entries)
		}
		batchIDs, err := queryBatch(client, entries[start:end])
		if err != nil {
			for i := start; i < end; i++ {
				results[i].err = err
			}
			continue
		}
		for i, ids := range batchIDs {
			results[start+i].ids = ids
		}
	}
	return results
}

func fetchVulnDetails(client *http.Client, uniqueIDs map[string]bool, onProgress ProgressFunc) map[string]osvVulnDetail {
	vulnDetails := make(map[string]osvVulnDetail)
	var mu sync.Mutex
	var completed int64
	totalWork := int64(len(uniqueIDs))

	idList := make([]string, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		idList = append(idList, id)
	}

	workers := concurrency
	if len(idList) < workers {
		workers = len(idList)
	}

	jobs := make(chan string, len(idList))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				detail, err := fetchVulnDetail(client, id)
				if err == nil {
					mu.Lock()
					vulnDetails[id] = detail
					mu.Unlock()
				}
				c := atomic.AddInt64(&completed, 1)
				if onProgress != nil {
					onProgress(int(c), int(totalWork))
				}
			}
		}()
	}

	for _, id := range idList {
		jobs <- id
	}
	close(jobs)
	wg.Wait()

	return vulnDetails
}

func queryBatch(client *http.Client, entries []batchEntry) ([][]string, error) {
	req := osvBatchRequest{
		Queries: make([]osvQuery, len(entries)),
	}
	for i, e := range entries {
		req.Queries[i] = osvQuery{
			Version: e.Version,
			Package: osvPackage{Name: e.Name, Ecosystem: "npm"},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal batch request: %w", err)
	}

	var resp osvBatchResponse
	if err := doRequestWithRetry(client, "POST", osvBatchURL, body, &resp); err != nil {
		return nil, fmt.Errorf("OSV batch query: %w", err)
	}

	result := make([][]string, len(entries))
	for i, r := range resp.Results {
		for _, v := range r.Vulns {
			result[i] = append(result[i], v.ID)
		}
	}
	return result, nil
}

func fetchVulnDetail(client *http.Client, id string) (osvVulnDetail, error) {
	url := fmt.Sprintf("%s/%s", osvVulnURL, id)
	var detail osvVulnDetail
	if err := doRequestWithRetry(client, "GET", url, nil, &detail); err != nil {
		return detail, err
	}
	return detail, nil
}

func doRequestWithRetry(client *http.Client, method, url string, reqBody []byte, out interface{}) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if reqBody != nil {
			bodyReader = bytes.NewReader(reqBody)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return err
		}
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(time.Duration(1<<uint(attempt)) * 500 * time.Millisecond)
			}
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			if attempt < maxRetries {
				time.Sleep(time.Duration(1<<uint(attempt)) * 500 * time.Millisecond)
			}
			continue
		}

		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
	return lastErr
}

func extractSeverity(detail osvVulnDetail) string {
	// Try database_specific.severity first (GHSA advisories include this)
	if ds := detail.DatabaseSpecific; ds != nil {
		if sev, ok := ds["severity"].(string); ok && sev != "" {
			return normalizeSeverity(sev)
		}
	}
	// Fall back to CVSS severity array
	for _, s := range detail.Severity {
		if level := cvssToLevel(s.Score); level != "" {
			return level
		}
	}
	return "UNKNOWN"
}

func extractAdvisoryURL(detail osvVulnDetail) string {
	for _, ref := range detail.References {
		if ref.Type == "ADVISORY" {
			return ref.URL
		}
	}
	for _, ref := range detail.References {
		if ref.Type == "WEB" {
			return ref.URL
		}
	}
	return ""
}

func normalizeSeverity(s string) string {
	switch s {
	case "CRITICAL", "critical":
		return "CRITICAL"
	case "HIGH", "high":
		return "HIGH"
	case "MODERATE", "moderate", "MEDIUM", "medium":
		return "MEDIUM"
	case "LOW", "low":
		return "LOW"
	default:
		return "UNKNOWN"
	}
}

// cvssToLevel extracts severity level from a CVSS v3 vector string.
// Format: CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H/...
// We approximate from the impact metrics since the actual score requires calculation.
func cvssToLevel(score string) string {
	if score == "" {
		return ""
	}
	high := 0
	for _, metric := range []string{"/C:", "/I:", "/A:"} {
		idx := len(score)
		for i := 0; i < len(score)-len(metric); i++ {
			if score[i:i+len(metric)] == metric {
				idx = i + len(metric)
				break
			}
		}
		if idx < len(score) {
			val := score[idx : idx+1]
			if val == "H" {
				high++
			}
		}
	}
	switch {
	case high >= 3:
		return "CRITICAL"
	case high >= 2:
		return "HIGH"
	case high >= 1:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
