// Package unused detects dependencies declared in package.json but not imported in source code.
package unused

import (
	"bufio"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// UnusedPackage represents a dependency that appears unused.
type UnusedPackage struct {
	Name    string
	Version string
}

// ScanResult holds the results of an unused dependency scan.
type ScanResult struct {
	Unused       []UnusedPackage
	Total        int
	ScannedFiles int
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
	Engines         map[string]string `json:"engines"`
	Bin             any               `json:"bin"`
}

// Source file extensions to scan for imports.
var sourceExts = map[string]bool{
	".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".mjs": true, ".cjs": true, ".vue": true, ".svelte": true,
}

// Config file patterns that may reference packages as plugins/presets.
var configFilePatterns = []string{
	// ESLint
	".eslintrc", ".eslintrc.js", ".eslintrc.cjs", ".eslintrc.json", ".eslintrc.yml", ".eslintrc.yaml",
	"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts",
	// Babel
	"babel.config.js", "babel.config.cjs", "babel.config.json", "babel.config.ts",
	".babelrc", ".babelrc.js", ".babelrc.cjs",
	// Webpack
	"webpack.config.js", "webpack.config.cjs", "webpack.config.ts", "webpack.config.mjs",
	// Jest
	"jest.config.js", "jest.config.cjs", "jest.config.ts", "jest.config.mjs", "jest.config.json",
	// TypeScript
	"tsconfig.json", "tsconfig.build.json",
	// Prettier
	".prettierrc", ".prettierrc.js", ".prettierrc.cjs", ".prettierrc.json",
	// PostCSS
	"postcss.config.js", "postcss.config.cjs", "postcss.config.mjs", "postcss.config.ts",
	// Vite
	"vite.config.js", "vite.config.ts", "vite.config.mjs",
	// Next.js
	"next.config.js", "next.config.mjs", "next.config.ts",
	// Tailwind
	"tailwind.config.js", "tailwind.config.ts", "tailwind.config.cjs", "tailwind.config.mjs",
	// Rollup
	"rollup.config.js", "rollup.config.mjs", "rollup.config.ts",
	// Vitest
	"vitest.config.js", "vitest.config.ts", "vitest.config.mjs",
	// Stylelint
	".stylelintrc", ".stylelintrc.js", ".stylelintrc.json", ".stylelintrc.ts",
	// Commitlint
	".commitlintrc", ".commitlintrc.js", ".commitlintrc.cjs", ".commitlintrc.json",
	".commitlintrc.ts", ".commitlintrc.yaml", ".commitlintrc.yml",
	"commitlint.config.js", "commitlint.config.cjs", "commitlint.config.ts", "commitlint.config.mjs",
	// Lint-staged
	".lintstagedrc", ".lintstagedrc.js", ".lintstagedrc.cjs", ".lintstagedrc.json",
	".lintstagedrc.ts", ".lintstagedrc.yaml", ".lintstagedrc.yml",
	"lint-staged.config.js", "lint-staged.config.cjs", "lint-staged.config.ts", "lint-staged.config.mjs",
	// Husky
	".huskyrc", ".huskyrc.js", ".huskyrc.json",
	// Nodemon
	"nodemon.json", ".nodemonrc", ".nodemonrc.json",
	// Turbo
	"turbo.json",
	// Mocha
	".mocharc.js", ".mocharc.cjs", ".mocharc.yaml", ".mocharc.yml", ".mocharc.json",
	// NYC / Istanbul
	".nycrc", ".nycrc.json", ".nycrc.yaml", ".nycrc.yml",
	// Sonar
	"sonar-project.properties",
}

// Directories to skip during scanning.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, ".nuxt": true, "coverage": true, ".cache": true,
	"out": true, ".turbo": true, ".vercel": true,
}

// Import/require patterns.
var (
	requireRe       = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	importFromRe    = regexp.MustCompile(`(?:import|export)\s+.*?\s+from\s+['"]([^'"]+)['"]`)
	importPlainRe   = regexp.MustCompile(`import\s+['"]([^'"]+)['"]`)
	dynamicImportRe = regexp.MustCompile(`import\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// Scan reads package.json, scans source files, and returns unused dependencies.
func Scan() (ScanResult, error) {
	result := ScanResult{}

	// Read package.json
	pkgData, err := os.ReadFile("package.json")
	if err != nil {
		return result, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return result, err
	}

	// Collect all declared dependencies with versions
	allDeps := make(map[string]string, len(pkg.Dependencies)+len(pkg.DevDependencies))
	maps.Copy(allDeps, pkg.Dependencies)
	maps.Copy(allDeps, pkg.DevDependencies)
	result.Total = len(allDeps)

	if len(allDeps) == 0 {
		return result, nil
	}

	// Collect used package names
	used := make(map[string]bool)

	// 1. Scan source files
	scannedFiles := 0
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if sourceExts[ext] {
			scannedFiles++
			extractImports(path, used)
		}
		return nil
	})

	// 2. Scan config files
	for _, cfgFile := range configFilePatterns {
		if _, err := os.Stat(cfgFile); err == nil {
			scannedFiles++
			extractImports(cfgFile, used)
			extractStringReferences(cfgFile, allDeps, used)
		}
	}

	// 3. Scan package.json scripts for CLI tool references
	for _, script := range pkg.Scripts {
		markCLIReferences(script, allDeps, used)
	}

	// 4. Scan .husky/ hooks for package references (npx commitlint, npx lint-staged, etc.)
	huskyDir := ".husky"
	if entries, err := os.ReadDir(huskyDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			hookPath := filepath.Join(huskyDir, entry.Name())
			if data, err := os.ReadFile(hookPath); err == nil {
				scannedFiles++
				content := string(data)
				// Check each line of the hook script for package references
				for _, line := range strings.Split(content, "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					markCLIReferences(line, allDeps, used)
				}
			}
		}
	}

	// 5. Special handling
	hasTypeScript := hasFileWithExt(".ts", ".tsx")

	for name := range allDeps {
		// @types/X → used if X is used
		if basePkg, ok := strings.CutPrefix(name, "@types/"); ok {
			// @types/node is always considered used
			if basePkg == "node" {
				used[name] = true
				continue
			}
			// Handle scoped types: @types/babel__core → @babel/core
			basePkg = strings.ReplaceAll(basePkg, "__", "/")
			if strings.Contains(basePkg, "/") {
				basePkg = "@" + basePkg
			}
			if used[basePkg] {
				used[name] = true
			}
		}

		// typescript → used if .ts/.tsx files exist
		if name == "typescript" && hasTypeScript {
			used[name] = true
		}
	}

	result.ScannedFiles = scannedFiles

	// Build unused list
	for name, ver := range allDeps {
		if !used[name] {
			result.Unused = append(result.Unused, UnusedPackage{
				Name:    name,
				Version: ver,
			})
		}
	}

	// Sort alphabetically
	sortUnused(result.Unused)

	return result, nil
}

// markCLIReferences checks if any dependency name (or its binary name) appears in a script/command string.
func markCLIReferences(script string, allDeps map[string]string, used map[string]bool) {
	for name := range allDeps {
		baseName := name
		if strings.HasPrefix(name, "@") {
			parts := strings.SplitN(name, "/", 2)
			if len(parts) == 2 {
				baseName = parts[1]
			}
		}
		if strings.Contains(script, baseName) {
			used[name] = true
		}
	}
}

// extractImports scans a file for require/import statements and adds package names to used.
func extractImports(path string, used map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		for _, re := range []*regexp.Regexp{requireRe, importFromRe, importPlainRe, dynamicImportRe} {
			for _, match := range re.FindAllStringSubmatch(line, -1) {
				if len(match) > 1 {
					pkg := extractPackageName(match[1])
					if pkg != "" {
						used[pkg] = true
					}
				}
			}
		}
	}
}

// extractStringReferences looks for package names mentioned as plain strings in config files.
// This catches plugin references like "prettier-plugin-tailwindcss" in config objects.
func extractStringReferences(path string, deps map[string]string, used map[string]bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	for name := range deps {
		if strings.Contains(content, name) {
			used[name] = true
		}
	}
}

// extractPackageName gets the npm package name from an import path.
// "lodash/get" → "lodash", "@scope/pkg/util" → "@scope/pkg", "./local" → ""
func extractPackageName(importPath string) string {
	// Skip relative imports
	if strings.HasPrefix(importPath, ".") || strings.HasPrefix(importPath, "/") {
		return ""
	}
	// Skip node built-ins
	if strings.HasPrefix(importPath, "node:") {
		return ""
	}

	// Scoped package: @scope/name/...
	if strings.HasPrefix(importPath, "@") {
		parts := strings.SplitN(importPath, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return importPath
	}

	// Regular package: name/...
	parts := strings.SplitN(importPath, "/", 2)
	return parts[0]
}

// hasFileWithExt checks if any source file with the given extensions exists.
func hasFileWithExt(exts ...string) bool {
	found := false
	filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if slices.Contains(exts, filepath.Ext(path)) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func sortUnused(pkgs []UnusedPackage) {
	for i := 1; i < len(pkgs); i++ {
		key := pkgs[i]
		j := i - 1
		for j >= 0 && pkgs[j].Name > key.Name {
			pkgs[j+1] = pkgs[j]
			j--
		}
		pkgs[j+1] = key
	}
}
