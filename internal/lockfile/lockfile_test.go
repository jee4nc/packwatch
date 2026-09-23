package lockfile

import (
	"os"
	"sort"
	"testing"
)

// writeProject creates package.json and package-lock.json in a temp dir and
// makes it the working directory.
func writeProject(t *testing.T, pkgJSON, lockJSON string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("package.json", []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if lockJSON != "" {
		if err := os.WriteFile("package-lock.json", []byte(lockJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type pkgSummary struct {
	Name    string
	Version string
	IsDev   bool
}

func summarize(pkgs []PackageInfo) []pkgSummary {
	var out []pkgSummary
	for _, p := range pkgs {
		out = append(out, pkgSummary{p.Name, p.Version.String(), p.IsDev})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func assertPackages(t *testing.T, got []PackageInfo, want []pkgSummary) {
	t.Helper()
	g := summarize(got)
	if len(g) != len(want) {
		t.Fatalf("got %d packages %+v, want %d %+v", len(g), g, len(want), want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("package %d = %+v, want %+v", i, g[i], want[i])
		}
	}
}

func TestParseV3(t *testing.T) {
	writeProject(t,
		`{
			"dependencies": {"express": "^4.18.0", "@scope/lib": "~1.2.0", "shared": "^1.0.0"},
			"devDependencies": {"vitest": "^1.0.0", "shared": "^1.0.0"},
			"engines": {"node": ">=18"}
		}`,
		`{
			"lockfileVersion": 3,
			"packages": {
				"": {"name": "app"},
				"node_modules/express": {"version": "4.18.2"},
				"node_modules/@scope/lib": {"version": "1.2.3"},
				"node_modules/shared": {"version": "1.0.1"},
				"node_modules/vitest": {"version": "1.6.0", "dev": true},
				"node_modules/express/node_modules/debug": {"version": "2.6.9"},
				"node_modules/transitive": {"version": "3.0.0"}
			}
		}`)

	res, err := Parse()
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if res.LockVersion != 3 {
		t.Errorf("LockVersion = %d, want 3", res.LockVersion)
	}
	if res.ProjectEngines.Node != ">=18" {
		t.Errorf("ProjectEngines.Node = %q, want >=18", res.ProjectEngines.Node)
	}
	assertPackages(t, res.Packages, []pkgSummary{
		{"@scope/lib", "1.2.3", false},
		{"express", "4.18.2", false},
		{"shared", "1.0.1", false}, // in both: prod wins
		{"vitest", "1.6.0", true},
	})

	ranges := map[string]string{}
	for _, p := range res.Packages {
		ranges[p.Name] = p.Range
	}
	for name, want := range map[string]string{"express": "^4.18.0", "@scope/lib": "~1.2.0", "shared": "^1.0.0", "vitest": "^1.0.0"} {
		if ranges[name] != want {
			t.Errorf("%s Range = %q, want %q", name, ranges[name], want)
		}
	}
}

func TestParseWorkspaces(t *testing.T) {
	writeProject(t,
		`{"workspaces": ["packages/*"], "dependencies": {"react": "^18.0.0"}}`,
		`{
			"lockfileVersion": 3,
			"packages": {
				"": {"name": "root", "workspaces": ["packages/*"]},
				"node_modules/react": {"version": "18.3.1"},
				"node_modules/ui": {"resolved": "packages/ui", "link": true},
				"packages/ui": {"version": "0.1.0"},
				"packages/ui/node_modules/react": {"version": "17.0.2"}
			}
		}`)

	res, err := Parse()
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	assertPackages(t, res.Packages, []pkgSummary{
		{"react", "18.3.1", false},
	})
}

func TestParseV1(t *testing.T) {
	writeProject(t,
		`{"dependencies": {"lodash": "^4.0.0"}, "devDependencies": {"mocha": "^10.0.0"}}`,
		`{
			"lockfileVersion": 1,
			"dependencies": {
				"lodash": {"version": "4.17.21"},
				"mocha": {"version": "10.2.0", "dev": true},
				"transitive": {"version": "1.0.0"}
			}
		}`)

	res, err := Parse()
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	assertPackages(t, res.Packages, []pkgSummary{
		{"lodash", "4.17.21", false},
		{"mocha", "10.2.0", true},
	})
}

func TestParseErrors(t *testing.T) {
	t.Run("missing lockfile", func(t *testing.T) {
		writeProject(t, `{}`, "")
		if _, err := Parse(); err == nil {
			t.Error("want error for missing package-lock.json")
		}
	})
	t.Run("invalid package.json", func(t *testing.T) {
		writeProject(t, `{not json`, `{"lockfileVersion": 3}`)
		if _, err := Parse(); err == nil {
			t.Error("want error for invalid package.json")
		}
	})
}

func TestExtractPackageName(t *testing.T) {
	tests := map[string]string{
		"node_modules/express":            "express",
		"node_modules/@scope/name":        "@scope/name",
		"node_modules/a/node_modules/b":   "",
		"packages/foo/node_modules/react": "",
		"packages/foo":                    "",
		"":                                "",
	}
	for key, want := range tests {
		if got := extractPackageName(key); got != want {
			t.Errorf("extractPackageName(%q) = %q, want %q", key, got, want)
		}
	}
}
