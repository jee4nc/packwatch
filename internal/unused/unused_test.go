package unused

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeFiles creates the given files (path → content) in a temp dir and makes
// it the working directory.
func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	t.Chdir(t.TempDir())
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func unusedNames(t *testing.T) []string {
	t.Helper()
	res, err := Scan()
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	var names []string
	for _, p := range res.Unused {
		names = append(names, p.Name)
	}
	return names
}

func TestScan(t *testing.T) {
	writeFiles(t, map[string]string{
		"package.json": `{
			"dependencies": {
				"express": "^4", "lodash": "^4", "@scope/pkg": "^1", "dayjs": "^1",
				"side-effect": "^1", "left-pad": "^1", "reexported": "^1"
			},
			"devDependencies": {
				"typescript": "^5", "@types/express": "^4", "@types/node": "^20",
				"@types/left-pad": "^1", "eslint": "^9", "eslint-plugin-react": "^7",
				"@typescript-eslint/eslint-plugin": "^8", "prettier": "^3",
				"husky": "^9", "lint-staged": "^15", "unused-dev": "^1"
			},
			"scripts": {"format": "prettier --write .", "prepare": "husky"}
		}`,
		"src/index.ts": `import express from 'express'
import get from "lodash/get"
import { x } from '@scope/pkg/sub'
import 'side-effect'
export { y } from 'reexported'
const d = await import("dayjs")
import fs from 'node:fs'
import local from './local'
`,
		"eslint.config.js":              `export default [{ plugins: { react: {} } }, "@typescript-eslint"]`,
		".husky/pre-commit":             "npx lint-staged\n",
		"node_modules/ignored/index.js": `require("left-pad")`,
		"dist/bundle.js":                `require("left-pad")`,
	})

	got := unusedNames(t)
	want := []string{"@types/left-pad", "left-pad", "unused-dev"}
	if !slices.Equal(got, want) {
		t.Errorf("unused = %v, want %v", got, want)
	}
}

func TestScanTypeScriptWithoutTSFiles(t *testing.T) {
	writeFiles(t, map[string]string{
		"package.json": `{"devDependencies": {"typescript": "^5"}}`,
		"index.js":     `console.log("hi")`,
	})
	if got := unusedNames(t); !slices.Equal(got, []string{"typescript"}) {
		t.Errorf("unused = %v, want [typescript]", got)
	}
}

func TestExtractPackageName(t *testing.T) {
	tests := map[string]string{
		"lodash":          "lodash",
		"lodash/get":      "lodash",
		"@scope/pkg":      "@scope/pkg",
		"@scope/pkg/deep": "@scope/pkg",
		"./local":         "",
		"/abs/path":       "",
		"node:fs":         "",
	}
	for in, want := range tests {
		if got := extractPackageName(in); got != want {
			t.Errorf("extractPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEslintShortNames(t *testing.T) {
	tests := map[string][]string{
		"eslint-plugin-react":              {"react"},
		"eslint-config-airbnb":             {"airbnb"},
		"@typescript-eslint/eslint-plugin": {"@typescript-eslint"},
		"@scope/eslint-plugin-foo":         {"@scope/foo"},
		"@scope/eslint-config":             {"@scope"},
		"lodash":                           nil,
	}
	for in, want := range tests {
		if got := eslintShortNames(in); !slices.Equal(got, want) {
			t.Errorf("eslintShortNames(%q) = %v, want %v", in, got, want)
		}
	}
}
