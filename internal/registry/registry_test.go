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

	var maxCompleted, lastTotal, callCount int64
	results := Fetch([]string{"a", "b", "c"}, cfg, func(completed, total int) {
		atomic.AddInt64(&callCount, 1)
		atomic.StoreInt64(&lastTotal, int64(total))
		for {
			cur := atomic.LoadInt64(&maxCompleted)
			if int64(completed) <= cur {
				break
			}
			if atomic.CompareAndSwapInt64(&maxCompleted, cur, int64(completed)) {
				break
			}
		}
	})

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	if c := atomic.LoadInt64(&callCount); c != 3 {
		t.Errorf("expected 3 progress callbacks, got %d", c)
	}
	if mc, lt := atomic.LoadInt64(&maxCompleted), atomic.LoadInt64(&lastTotal); mc != 3 || lt != 3 {
		t.Errorf("max progress = (%d, %d), want (3, 3)", mc, lt)
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

func testConfig(url string) npmrc.Config {
	return npmrc.Config{
		DefaultRegistry: url,
		ScopeRegistries: make(map[string]string),
		AuthTokens:      make(map[string]string),
	}
}

func TestFetch_NoRetryOnClientError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnauthorized} {
		attempts := int64(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&attempts, 1)
			w.WriteHeader(status)
		}))

		results := Fetch([]string{"missing"}, testConfig(server.URL), nil)
		server.Close()

		if results[0].Error == nil {
			t.Errorf("HTTP %d: expected error", status)
		}
		if attempts != 1 {
			t.Errorf("HTTP %d: got %d attempts, want 1 (no retry)", status, attempts)
		}
	}
}

func TestFetch_EscapesScopedName(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(registryResponse{
			DistTags: map[string]string{"latest": "1.0.0"},
			Versions: map[string]registryVersion{"1.0.0": {Version: "1.0.0"}},
		})
	}))
	defer server.Close()

	results := Fetch([]string{"@scope/name"}, testConfig(server.URL), nil)
	if results[0].Error != nil {
		t.Fatalf("unexpected error: %v", results[0].Error)
	}
	if gotPath != "/@scope%2fname" {
		t.Errorf("request path = %q, want /@scope%%2fname", gotPath)
	}
}

func TestFetch_ParsesDeprecated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"dist-tags": {"latest": "1.2.0"},
			"versions": {
				"1.0.0": {"version": "1.0.0", "deprecated": false},
				"1.1.0": {"version": "1.1.0", "deprecated": "critical bug, use 1.2.0"},
				"1.2.0": {"version": "1.2.0", "deprecated": ""}
			}
		}`))
	}))
	defer server.Close()

	pv := Fetch([]string{"pkg"}, testConfig(server.URL), nil)[0]
	if pv.Error != nil {
		t.Fatalf("unexpected error: %v", pv.Error)
	}
	for v, want := range map[string]bool{"1.0.0": false, "1.1.0": true, "1.2.0": false} {
		if pv.Deprecated[v] != want {
			t.Errorf("Deprecated[%s] = %v, want %v", v, pv.Deprecated[v], want)
		}
	}
}

func TestFindCompatible_SkipsDeprecatedAndAboveLatest(t *testing.T) {
	v18 := mustParse("18.0.0")
	pv := PackageVersions{
		Name:     "pkg",
		Latest:   mustParse("2.0.0"),
		Versions: []semver.Version{mustParse("1.0.0"), mustParse("1.1.0"), mustParse("2.0.0"), mustParse("3.0.0")},
		Engines:  map[string]string{"2.0.0": ">=20"},
		// 3.0.0 is published under another dist-tag (e.g. "next")
		Deprecated: map[string]bool{"1.1.0": true},
	}

	got, ok := FindCompatibleLatest(pv, v18)
	if !ok || got.String() != "1.0.0" {
		t.Errorf("FindCompatibleLatest = %s, %v; want 1.0.0 (skips 3.0.0 above latest, 2.0.0 needs node 20, 1.1.0 deprecated)", got, ok)
	}

	got, ok = FindPeerCompatibleLatest(pv, mustParse("22.0.0"), []string{">=1"})
	if !ok || got.String() != "2.0.0" {
		t.Errorf("FindPeerCompatibleLatest = %s, %v; want 2.0.0 (capped at latest)", got, ok)
	}
}
