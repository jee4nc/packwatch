// Package node detects the active Node.js version from various sources.
package node

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jee4nc/packwatch/internal/semver"
)

// Detection holds the detected Node version and where it came from.
type Detection struct {
	Version semver.Version
	Source  string // e.g. ".nvmrc", ".node-version", "node --version"
}

// Detect finds the active Node.js version by checking, in order:
// .nvmrc → .node-version → node --version
func Detect() (Detection, error) {
	// 1. .nvmrc
	if v, err := readVersionFile(".nvmrc"); err == nil {
		return Detection{Version: v, Source: ".nvmrc"}, nil
	}

	// 2. .node-version
	if v, err := readVersionFile(".node-version"); err == nil {
		return Detection{Version: v, Source: ".node-version"}, nil
	}

	// 3. node --version
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return Detection{}, fmt.Errorf("could not detect Node.js version: no .nvmrc, .node-version, or node binary found")
	}
	v, err := semver.Parse(strings.TrimSpace(string(out)))
	if err != nil {
		return Detection{}, fmt.Errorf("could not parse node --version output: %w", err)
	}
	return Detection{Version: v, Source: "node --version"}, nil
}

func readVersionFile(name string) (semver.Version, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return semver.Version{}, err
	}
	s := strings.TrimSpace(string(data))
	// Handle nvm aliases like "lts/*", "stable", etc.
	if s == "" || strings.ContainsAny(s, "/*") {
		return semver.Version{}, fmt.Errorf("unsupported version format: %s", s)
	}
	return semver.Parse(s)
}
