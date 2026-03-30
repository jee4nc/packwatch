// Package lockfile parses package-lock.json (v2 and v3) and package.json.
package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jee4nc/packwatch/internal/semver"
)

// PackageInfo represents a dependency extracted from the lockfile.
type PackageInfo struct {
	Name    string
	Version semver.Version
	IsDev   bool
	InLock  bool // present in the lockfile
}

// ProjectEngines holds the engines constraint from package.json.
type ProjectEngines struct {
	Node string
}

// ParseResult holds the parsed lockfile data.
type ParseResult struct {
	Packages       []PackageInfo
	LockVersion    int
	ProjectEngines ProjectEngines
}

// lockfileJSON is the raw structure of package-lock.json.
type lockfileJSON struct {
	LockfileVersion int                          `json:"lockfileVersion"`
	Packages        map[string]lockfilePackage   `json:"packages"`
	Dependencies    map[string]lockfileLegacyDep `json:"dependencies"`
}

type lockfilePackage struct {
	Version     string `json:"version"`
	Dev         bool   `json:"dev"`
	DevOptional bool   `json:"devOptional"`
	Link        bool   `json:"link"`
}

type lockfileLegacyDep struct {
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
}

// packageJSON is the raw structure of package.json.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// Parse reads and parses package-lock.json and package.json from the current directory.
func Parse() (ParseResult, error) {
	result := ParseResult{}

	// Read package.json for engines and dep classification
	pkgData, err := os.ReadFile("package.json")
	if err != nil {
		return result, fmt.Errorf("cannot read package.json: %w", err)
	}
	var pkg packageJSON
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return result, fmt.Errorf("cannot parse package.json: %w", err)
	}
	result.ProjectEngines = ProjectEngines{Node: pkg.Engines.Node}

	// Build sets of dep names from package.json for classification
	prodDeps := make(map[string]bool)
	devDeps := make(map[string]bool)
	for name := range pkg.Dependencies {
		prodDeps[name] = true
	}
	for name := range pkg.DevDependencies {
		devDeps[name] = true
	}

	// Read package-lock.json
	lockData, err := os.ReadFile("package-lock.json")
	if err != nil {
		return result, fmt.Errorf("cannot read package-lock.json: %w", err)
	}
	var lock lockfileJSON
	if err := json.Unmarshal(lockData, &lock); err != nil {
		return result, fmt.Errorf("cannot parse package-lock.json: %w", err)
	}
	result.LockVersion = lock.LockfileVersion

	// v2 and v3 use "packages" map
	if lock.LockfileVersion >= 2 && lock.Packages != nil {
		for key, pkg := range lock.Packages {
			// Skip the root entry (empty key)
			if key == "" {
				continue
			}
			// Skip workspace links
			if pkg.Link {
				continue
			}
			// Extract package name from the key (e.g., "node_modules/@scope/name")
			name := extractPackageName(key)
			if name == "" {
				continue
			}
			// Only include direct dependencies (in package.json)
			isProd := prodDeps[name]
			isDev := devDeps[name]
			if !isProd && !isDev {
				continue
			}

			v, err := semver.Parse(pkg.Version)
			if err != nil {
				continue
			}
			result.Packages = append(result.Packages, PackageInfo{
				Name:    name,
				Version: v,
				IsDev:   isDev && !isProd,
				InLock:  true,
			})
		}
	} else if lock.Dependencies != nil {
		// Fallback for v1 (though we claim v2/v3 support, handle gracefully)
		for name, dep := range lock.Dependencies {
			isProd := prodDeps[name]
			isDev := devDeps[name]
			if !isProd && !isDev {
				continue
			}
			v, err := semver.Parse(dep.Version)
			if err != nil {
				continue
			}
			result.Packages = append(result.Packages, PackageInfo{
				Name:    name,
				Version: v,
				IsDev:   isDev && !isProd,
				InLock:  true,
			})
		}
	}

	return result, nil
}

// extractPackageName gets the package name from a node_modules path key.
// e.g., "node_modules/express" → "express"
//
//	"node_modules/@scope/name" → "@scope/name"
//	"node_modules/a/node_modules/b" → skip (nested dep)
func extractPackageName(key string) string {
	// Skip nested dependencies (transitive)
	parts := strings.Split(key, "node_modules/")
	if len(parts) > 2 {
		return "" // nested dependency, skip
	}
	// Get the last segment
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, "/")
	return name
}
