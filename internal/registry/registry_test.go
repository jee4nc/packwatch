package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jee4nc/packwatch/internal/npmrc"
	"github.com/jee4nc/packwatch/internal/semver"
)

func TestFetch_WorkerPoolLimitsGoroutines(t *testing.T) {
	var maxConcurrent int64
	var current int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt64(&current, 1)
		for {
			old := atomic.LoadInt64(&maxConcurrent)
			if c <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, c) {
				break
			}
		}
		defer atomic.AddInt64(&current, -1)

		resp := registryResponse{
			DistTags: map[string]string{"latest": "1.0.0"},
			Versions: map[string]registryVersion{
				"1.0.0": {Version: "1.0.0"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	names := make([]string, 50)
	for i := range names {
		names[i] = "pkg-" + string(rune('a'+i%26))
	}

	cfg := npmrc.Config{
		DefaultRegistry: server.URL,
		ScopeRegistries: make(map[string]string),
		AuthTokens:      make(map[string]string),
	}

	results := Fetch(names, cfg, nil)

	if len(results) != 50 {
		t.Errorf("expected 50 results, got %d", len(results))
	}
	if maxConcurrent > int64(concurrency) {
		t.Errorf("max concurrent requests was %d, should be <= %d", maxConcurrent, concurrency)
	}
}

func TestFetch_RetriesOnFailure(t *testing.T) {
	attempts := int64(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt64(&attempts, 1)
		if attempt <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := registryResponse{
			DistTags: map[string]string{"latest": "2.0.0"},
			Versions: map[string]registryVersion{
				"2.0.0": {Version: "2.0.0"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := npmrc.Config{
		DefaultRegistry: server.URL,
		ScopeRegistries: make(map[string]string),
		AuthTokens:      make(map[string]string),
	}

	results := Fetch([]string{"test-pkg"}, cfg, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("expected success after retries, got error: %v", results[0].Error)
	}
	if results[0].Latest.String() != "2.0.0" {
		t.Errorf("expected latest 2.0.0, got %s", results[0].Latest.String())
	}
	if atomic.LoadInt64(&attempts) < 3 {
		t.Errorf("expected at least 3 attempts, got %d", atomic.LoadInt64(&attempts))
	}
}

func TestFetch_ProgressCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := registryResponse{
			DistTags: map[string]string{"latest": "1.0.0"},
			Versions: map[string]registryVersion{
				"1.0.0": {Version: "1.0.0"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := npmrc.Config{
		DefaultRegistry: server.URL,
		ScopeRegistries: make(map[string]string),
		AuthTokens:      make(map[string]string),
	}

	var lastCompleted, lastTotal int
	callCount := 0
	results := Fetch([]string{"a", "b", "c"}, cfg, func(completed, total int) {
		lastCompleted = completed
		lastTotal = total
		callCount++
	})

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	if callCount != 3 {
		t.Errorf("expected 3 progress callbacks, got %d", callCount)
	}
	if lastCompleted != 3 || lastTotal != 3 {
		t.Errorf("final progress = (%d, %d), want (3, 3)", lastCompleted, lastTotal)
	}
}

func TestFindCompatibleLatest(t *testing.T) {
	v18, _ := semver.Parse("18.0.0")
	v16, _ := semver.Parse("16.0.0")

	pv := PackageVersions{
		Name: "test-pkg",
		Versions: []semver.Version{
			mustParse("1.0.0"),
			mustParse("2.0.0"),
			mustParse("3.0.0"),
			mustParse("4.0.0"),
		},
		Engines: map[string]string{
			"4.0.0": ">=20.0.0",
			"3.0.0": ">=18.0.0",
			"2.0.0": ">=16.0.0",
		},
	}

	got, ok := FindCompatibleLatest(pv, v18)
	if !ok {
		t.Fatal("expected to find compatible version")
	}
	if got.String() != "3.0.0" {
		t.Errorf("expected 3.0.0 for Node 18, got %s", got.String())
	}

	got, ok = FindCompatibleLatest(pv, v16)
	if !ok {
		t.Fatal("expected to find compatible version")
	}
	if got.String() != "2.0.0" {
		t.Errorf("expected 2.0.0 for Node 16, got %s", got.String())
	}
}

func TestFindCompatibleLatest_NoConstraintMeansCompatible(t *testing.T) {
	v14, _ := semver.Parse("14.0.0")

	pv := PackageVersions{
		Name: "test-pkg",
		Versions: []semver.Version{
			mustParse("1.0.0"),
			mustParse("2.0.0"),
		},
		Engines: map[string]string{},
	}

	got, ok := FindCompatibleLatest(pv, v14)
	if !ok {
		t.Fatal("expected to find compatible version")
	}
	if got.String() != "2.0.0" {
		t.Errorf("expected 2.0.0 (newest without constraint), got %s", got.String())
	}
}

func TestParseNodeEngine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"object form", `{"node": ">=16.0.0"}`, ">=16.0.0"},
		{"multiple engines", `{"node": ">=18", "npm": ">=8"}`, ">=18"},
		{"no node field", `{"npm": ">=8"}`, ""},
		{"empty", ``, ""},
		{"invalid json", `{invalid`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNodeEngine(json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("parseNodeEngine(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	versions := []semver.Version{
		mustParse("3.0.0"),
		mustParse("1.0.0"),
		mustParse("2.0.0"),
		mustParse("1.5.0"),
	}

	sortVersions(versions)

	expected := []string{"1.0.0", "1.5.0", "2.0.0", "3.0.0"}
	for i, v := range versions {
		if v.String() != expected[i] {
			t.Errorf("sortVersions: index %d = %s, want %s", i, v.String(), expected[i])
		}
	}
}

func mustParse(s string) semver.Version {
	v, err := semver.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}
