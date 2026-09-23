package node

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

const releaseIndex = `[
	{"version": "v22.12.0"},
	{"version": "v20.19.5"},
	{"version": "v20.18.3"},
	{"version": "v20.18.0"},
	{"version": "v18.20.8"}
]`

// setup runs the test in an empty temp dir with the given version files,
// a stubbed `node --version`, and a fake release index.
func setup(t *testing.T, files map[string]string, active string, indexUp bool) {
	t.Helper()
	t.Chdir(t.TempDir())
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	oldActive, oldURL := activeVersion, releasesURL
	t.Cleanup(func() { activeVersion, releasesURL = oldActive, oldURL })
	activeVersion = func() (string, error) {
		if active == "" {
			return "", errors.New("node not found")
		}
		return active, nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !indexUp {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(releaseIndex))
	}))
	t.Cleanup(srv.Close)
	releasesURL = srv.URL
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		active      string
		indexUp     bool
		wantVersion string
		wantSource  string
	}{
		{
			name:        "full version in .nvmrc",
			files:       map[string]string{".nvmrc": "20.11.1\n"},
			active:      "v22.12.0",
			wantVersion: "20.11.1",
			wantSource:  ".nvmrc",
		},
		{
			name:        "partial .nvmrc matches active node",
			files:       map[string]string{".nvmrc": "20"},
			active:      "v20.18.0",
			indexUp:     true,
			wantVersion: "20.18.0",
			wantSource:  ".nvmrc 20 → node --version",
		},
		{
			name:        "partial .nvmrc with v prefix",
			files:       map[string]string{".nvmrc": "v20"},
			active:      "v20.18.0",
			wantVersion: "20.18.0",
			wantSource:  ".nvmrc v20 → node --version",
		},
		{
			name:        "partial .nvmrc, active node in another line, resolved from index",
			files:       map[string]string{".nvmrc": "20"},
			active:      "v18.20.8",
			indexUp:     true,
			wantVersion: "20.19.5",
			wantSource:  ".nvmrc 20 → latest release",
		},
		{
			name:        "major.minor spec respects minor",
			files:       map[string]string{".nvmrc": "20.18"},
			active:      "v20.19.5",
			indexUp:     true,
			wantVersion: "20.18.3",
			wantSource:  ".nvmrc 20.18 → latest release",
		},
		{
			name:        "partial spec, no node and index down, falls back to x.0.0",
			files:       map[string]string{".nvmrc": "20"},
			wantVersion: "20.0.0",
			wantSource:  ".nvmrc 20, assumed 20.0.0",
		},
		{
			name:        ".node-version used when no .nvmrc",
			files:       map[string]string{".node-version": "22.12.0"},
			active:      "v18.20.8",
			wantVersion: "22.12.0",
			wantSource:  ".node-version",
		},
		{
			name:        "nvm alias falls back to node --version",
			files:       map[string]string{".nvmrc": "lts/*"},
			active:      "v22.12.0",
			wantVersion: "22.12.0",
			wantSource:  "node --version",
		},
		{
			name:        "no version files",
			active:      "v22.12.0",
			wantVersion: "22.12.0",
			wantSource:  "node --version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t, tt.files, tt.active, tt.indexUp)

			d, err := Detect()
			if err != nil {
				t.Fatalf("Detect() error: %v", err)
			}
			if d.Version.String() != tt.wantVersion {
				t.Errorf("Version = %s, want %s", d.Version, tt.wantVersion)
			}
			if d.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", d.Source, tt.wantSource)
			}
		})
	}
}

func TestDetectNoNode(t *testing.T) {
	setup(t, nil, "", false)
	if _, err := Detect(); err == nil {
		t.Fatal("Detect() with no version files and no node binary: want error")
	}
}
