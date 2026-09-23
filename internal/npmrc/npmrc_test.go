package npmrc

import (
	"os"
	"path/filepath"
	"testing"
)

// setup isolates the user-level ~/.npmrc in a temp HOME and runs the test in
// a temp project dir.
func setup(t *testing.T, userRC, projectRC string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if userRC != "" {
		if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte(userRC), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
	if projectRC != "" {
		if err := os.WriteFile(".npmrc", []byte(projectRC), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseDefaults(t *testing.T) {
	setup(t, "", "")
	cfg := Parse()
	if got := cfg.RegistryFor("express"); got != "https://registry.npmjs.org" {
		t.Errorf("RegistryFor(express) = %q", got)
	}
	if got := cfg.RegistryFor("@types/node"); got != "https://registry.npmjs.org" {
		t.Errorf("RegistryFor(@types/node) = %q", got)
	}
	if cfg.Summary() != "" {
		t.Errorf("Summary() = %q, want empty", cfg.Summary())
	}
}

func TestParseScopesAndTokens(t *testing.T) {
	t.Setenv("NPM_TOKEN", "secret-from-env")
	setup(t,
		`registry=https://user.example.com/
@corp:registry=https://user.example.com/npm/
//user.example.com/npm/:_authToken=user-token
`,
		`# project overrides
; also a comment
@corp:registry=https://npm.corp.example.com/api/npm/
//npm.corp.example.com/api/npm/:_authToken=${NPM_TOKEN}
//other.example.com/:_authToken=host-token
`)
	cfg := Parse()

	if got := cfg.RegistryFor("lodash"); got != "https://user.example.com" {
		t.Errorf("RegistryFor(lodash) = %q, want user-level default registry", got)
	}
	corp := cfg.RegistryFor("@corp/ui")
	if corp != "https://npm.corp.example.com/api/npm" {
		t.Errorf("RegistryFor(@corp/ui) = %q, want project-level scope registry", corp)
	}
	if got := cfg.AuthTokenFor(corp); got != "secret-from-env" {
		t.Errorf("AuthTokenFor(%s) = %q, want env-expanded token", corp, got)
	}
	if got := cfg.AuthTokenFor("https://other.example.com/some/path"); got != "host-token" {
		t.Errorf("AuthTokenFor(other host) = %q, want host-level token", got)
	}
	if got := cfg.AuthTokenFor("https://registry.npmjs.org"); got != "" {
		t.Errorf("AuthTokenFor(npmjs) = %q, want no token", got)
	}
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("A", "1")
	t.Setenv("B", "2")
	tests := map[string]string{
		"${A}":        "1",
		"x-${A}-${B}": "x-1-2",
		"${MISSING}":  "",
		"no vars":     "no vars",
		"${UNCLOSED":  "${UNCLOSED",
	}
	for in, want := range tests {
		if got := expandEnvVars(in); got != want {
			t.Errorf("expandEnvVars(%q) = %q, want %q", in, got, want)
		}
	}
}
