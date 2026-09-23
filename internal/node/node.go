// Package node detects the active Node.js version from various sources.
package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jee4nc/packwatch/internal/semver"
)

// Detection holds the detected Node version and where it came from.
type Detection struct {
	Version semver.Version
	Source  string // e.g. ".nvmrc", ".node-version", "node --version"
}

// Overridable in tests.
var (
	releasesURL = "https://nodejs.org/dist/index.json"

	activeVersion = func() (string, error) {
		out, err := exec.Command("node", "--version").Output()
		return strings.TrimSpace(string(out)), err
	}
)

// spec is a version read from a version file. parts is the number of
// components given: "20" → 1, "20.11" → 2, "20.11.1" → 3.
type spec struct {
	raw     string
	version semver.Version
	parts   int
}

// Detect finds the active Node.js version by checking, in order:
// .nvmrc → .node-version → node --version
//
// A partial version in a file ("20", "20.11") is resolved the way version
// managers install it: to the active node if it is in that line, otherwise
// to the latest release in that line.
func Detect() (Detection, error) {
	for _, name := range []string{".nvmrc", ".node-version"} {
		if s, err := readVersionFile(name); err == nil {
			return resolve(s, name), nil
		}
	}

	out, err := activeVersion()
	if err != nil {
		return Detection{}, fmt.Errorf("could not detect Node.js version: no .nvmrc, .node-version, or node binary found")
	}
	v, err := semver.Parse(out)
	if err != nil {
		return Detection{}, fmt.Errorf("could not parse node --version output: %w", err)
	}
	return Detection{Version: v, Source: "node --version"}, nil
}

func resolve(s spec, source string) Detection {
	if s.parts == 3 {
		return Detection{Version: s.version, Source: source}
	}

	if out, err := activeVersion(); err == nil {
		if v, err := semver.Parse(out); err == nil && s.matches(v) {
			return Detection{Version: v, Source: fmt.Sprintf("%s %s → node --version", source, s.raw)}
		}
	}

	if v, err := latestInLine(s); err == nil {
		return Detection{Version: v, Source: fmt.Sprintf("%s %s → latest release", source, s.raw)}
	}

	return Detection{Version: s.version, Source: fmt.Sprintf("%s %s, assumed %s", source, s.raw, s.version)}
}

// matches reports whether v is in the release line described by the spec.
func (s spec) matches(v semver.Version) bool {
	if v.Prerelease != "" || v.Major != s.version.Major {
		return false
	}
	return s.parts < 2 || v.Minor == s.version.Minor
}

// latestInLine returns the newest Node release matching a partial spec,
// using the official release index.
func latestInLine(s spec) (semver.Version, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(releasesURL)
	if err != nil {
		return semver.Version{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return semver.Version{}, fmt.Errorf("HTTP %d from %s", resp.StatusCode, releasesURL)
	}

	var releases []struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return semver.Version{}, fmt.Errorf("decode release index: %w", err)
	}

	var best semver.Version
	found := false
	for _, r := range releases {
		v, err := semver.Parse(r.Version)
		if err != nil || !s.matches(v) {
			continue
		}
		if !found || best.LessThan(v) {
			best, found = v, true
		}
	}
	if !found {
		return semver.Version{}, fmt.Errorf("no Node release matches %s", s.raw)
	}
	return best, nil
}

func readVersionFile(name string) (spec, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return spec{}, err
	}
	s := strings.TrimSpace(string(data))
	// Handle nvm aliases like "lts/*", "stable", etc.
	if s == "" || strings.ContainsAny(s, "/*") {
		return spec{}, fmt.Errorf("unsupported version format: %s", s)
	}
	v, err := semver.Parse(s)
	if err != nil {
		return spec{}, err
	}
	core, _, _ := strings.Cut(strings.TrimPrefix(s, "v"), "-")
	return spec{raw: s, version: v, parts: strings.Count(core, ".") + 1}, nil
}
