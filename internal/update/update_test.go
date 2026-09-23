package update

import (
	"testing"

	"github.com/jee4nc/packwatch/internal/registry"
	"github.com/jee4nc/packwatch/internal/semver"
)

func mustParse(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

// pkg builds registry metadata from ascending versions; engines maps
// version → engines.node constraint.
func pkg(t *testing.T, versions []string, engines map[string]string) registry.PackageVersions {
	t.Helper()
	pv := registry.PackageVersions{Engines: map[string]string{}}
	for _, s := range versions {
		pv.Versions = append(pv.Versions, mustParse(t, s))
	}
	pv.Latest = pv.Versions[len(pv.Versions)-1]
	for v, e := range engines {
		pv.Engines[v] = e
	}
	return pv
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name          string
		installed     string
		versions      []string
		engines       map[string]string
		node          string
		peers         []PeerConstraint
		wantAvailable string
		wantType      semver.UpdateType
		wantNodeWarn  bool
		wantPeerWarn  bool
		wantHeldBack  bool
	}{
		{
			name:          "already on latest",
			installed:     "2.0.0",
			versions:      []string{"1.0.0", "2.0.0"},
			node:          "20.0.0",
			wantAvailable: "2.0.0",
			wantType:      semver.UpToDate,
		},
		{
			name:          "plain minor update",
			installed:     "1.0.0",
			versions:      []string{"1.0.0", "1.1.0"},
			node:          "20.0.0",
			wantAvailable: "1.1.0",
			wantType:      semver.Minor,
		},
		{
			name:          "latest needs newer node, older compatible version suggested",
			installed:     "1.0.0",
			versions:      []string{"1.0.0", "1.5.0", "2.0.0"},
			engines:       map[string]string{"1.5.0": ">=18", "2.0.0": ">=20"},
			node:          "18.0.0",
			wantAvailable: "1.5.0",
			wantType:      semver.Minor,
			wantNodeWarn:  true,
		},
		{
			name:          "latest needs newer node, installed is newest compatible",
			installed:     "1.5.0",
			versions:      []string{"1.0.0", "1.5.0", "2.0.0"},
			engines:       map[string]string{"1.5.0": ">=18", "2.0.0": ">=20"},
			node:          "18.0.0",
			wantAvailable: "2.0.0",
			wantType:      semver.UpToDate,
			wantNodeWarn:  true,
			wantHeldBack:  true,
		},
		{
			name:          "no version compatible with node",
			installed:     "1.0.0",
			versions:      []string{"1.0.0", "2.0.0"},
			engines:       map[string]string{"1.0.0": ">=20", "2.0.0": ">=20"},
			node:          "18.0.0",
			wantAvailable: "2.0.0",
			wantType:      semver.UpToDate,
			wantNodeWarn:  true,
			wantHeldBack:  true,
		},
		{
			name:          "peer deps satisfied",
			installed:     "18.2.0",
			versions:      []string{"18.2.0", "18.3.1"},
			node:          "20.0.0",
			peers:         []PeerConstraint{{Constraint: "^18.0.0", Source: "react-dom"}},
			wantAvailable: "18.3.1",
			wantType:      semver.Minor,
		},
		{
			name:          "peer conflict, older satisfying version suggested",
			installed:     "18.2.0",
			versions:      []string{"18.2.0", "18.3.1", "19.0.0"},
			node:          "20.0.0",
			peers:         []PeerConstraint{{Constraint: "^18.0.0", Source: "react-dom"}},
			wantAvailable: "18.3.1",
			wantType:      semver.Minor,
			wantPeerWarn:  true,
		},
		{
			name:          "peer conflict, installed is newest satisfying",
			installed:     "18.3.1",
			versions:      []string{"18.2.0", "18.3.1", "19.0.0"},
			node:          "20.0.0",
			peers:         []PeerConstraint{{Constraint: "16.x || 17.x || 18.x", Source: "some-lib"}},
			wantAvailable: "19.0.0",
			wantType:      semver.UpToDate,
			wantPeerWarn:  true,
			wantHeldBack:  true,
		},
		{
			name:          "node fallback then peer check on the fallback",
			installed:     "1.0.0",
			versions:      []string{"1.0.0", "1.1.0", "1.2.0", "2.0.0"},
			engines:       map[string]string{"2.0.0": ">=22"},
			node:          "20.0.0",
			peers:         []PeerConstraint{{Constraint: "~1.1.0", Source: "plugin"}},
			wantAvailable: "1.1.0",
			wantType:      semver.Minor,
			wantNodeWarn:  true,
			wantPeerWarn:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := pkg(t, tt.versions, tt.engines)
			d := Decide(mustParse(t, tt.installed), reg, mustParse(t, tt.node), tt.peers)

			if d.Available != tt.wantAvailable {
				t.Errorf("Available = %s, want %s", d.Available, tt.wantAvailable)
			}
			if d.UpdateType != tt.wantType {
				t.Errorf("UpdateType = %s, want %s", d.UpdateType, tt.wantType)
			}
			if (d.NodeWarning != "") != tt.wantNodeWarn {
				t.Errorf("NodeWarning = %q, want warning: %v", d.NodeWarning, tt.wantNodeWarn)
			}
			if (d.PeerWarning != "") != tt.wantPeerWarn {
				t.Errorf("PeerWarning = %q, want warning: %v", d.PeerWarning, tt.wantPeerWarn)
			}
			if d.HeldBack() != tt.wantHeldBack {
				t.Errorf("HeldBack() = %v, want %v", d.HeldBack(), tt.wantHeldBack)
			}
		})
	}
}
