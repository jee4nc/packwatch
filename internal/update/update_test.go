package update

import (
	"slices"
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
			d := Decide(mustParse(t, tt.installed), reg, Options{Node: mustParse(t, tt.node)}, Env{Peers: tt.peers})

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

func TestDecideOwnPeerDeps(t *testing.T) {
	// plugin@2 needs host ^2, but host stays on 1.x: suggest the newest plugin that accepts host 1
	reg := pkg(t, []string{"1.0.0", "1.4.0", "2.0.0"}, nil)
	reg.PeerDeps = map[string]map[string]string{
		"1.4.0": {"host": "^1.0.0"},
		"2.0.0": {"host": "^2.0.0"},
	}
	env := Env{Others: map[string]semver.Version{"host": mustParse(t, "1.9.0")}}
	d := Decide(mustParse(t, "1.0.0"), reg, Options{Node: mustParse(t, "22.0.0")}, env)
	if d.Available != "1.4.0" || d.PeerWarning == "" {
		t.Errorf("got %s (warning %q), want 1.4.0 with a peer warning", d.Available, d.PeerWarning)
	}
}

// planPkg builds a Package whose every version pins peers, given as
// version → peer name → constraint.
func planPkg(t *testing.T, name, installed string, versions []string, engines map[string]string, peers map[string]map[string]string) Package {
	t.Helper()
	reg := pkg(t, versions, engines)
	reg.Name = name
	reg.PeerDeps = peers
	return Package{Name: name, Installed: mustParse(t, installed), Registry: reg}
}

func TestPlanMutualPeers(t *testing.T) {
	versions := []string{"4.0.0", "5.0.1"}
	pkgs := []Package{
		planPkg(t, "vitest", "4.0.0", versions, nil, map[string]map[string]string{
			"4.0.0": {"@vitest/coverage-v8": "4.0.0"},
			"5.0.1": {"@vitest/coverage-v8": "5.0.1"},
		}),
		planPkg(t, "@vitest/coverage-v8", "4.0.0", versions, nil, map[string]map[string]string{
			"4.0.0": {"vitest": "4.0.0"},
			"5.0.1": {"vitest": "5.0.1"},
		}),
		planPkg(t, "unrelated", "1.0.0", []string{"1.0.0", "1.1.0"}, nil, nil),
	}
	node := Options{Node: mustParse(t, "22.0.0")}

	// Deciding against installed versions alone holds both back
	for i, p := range pkgs[:2] {
		other := pkgs[1-i]
		env := envFor(p.Name, pkgs, map[string]semver.Version{p.Name: p.Installed, other.Name: other.Installed, "unrelated": pkgs[2].Installed})
		if d := Decide(p.Installed, p.Registry, node, env); !d.HeldBack() {
			t.Fatalf("%s alone: want held back, got %+v", p.Name, d)
		}
	}

	ds := Plan(pkgs, node)
	want := []struct {
		available string
		requires  []string
	}{
		{"5.0.1", []string{"@vitest/coverage-v8"}},
		{"5.0.1", []string{"vitest"}},
		{"1.1.0", nil},
	}
	for i, w := range want {
		if ds[i].Available != w.available || ds[i].UpdateType == semver.UpToDate {
			t.Errorf("%s: got %s (%s), want update to %s", pkgs[i].Name, ds[i].Available, ds[i].UpdateType, w.available)
		}
		if !slices.Equal(ds[i].RequiresWith, w.requires) {
			t.Errorf("%s: RequiresWith = %v, want %v", pkgs[i].Name, ds[i].RequiresWith, w.requires)
		}
		if ds[i].PeerWarning != "" {
			t.Errorf("%s: unexpected PeerWarning %q", pkgs[i].Name, ds[i].PeerWarning)
		}
	}
}

func TestPlanPartnerBlockedByNode(t *testing.T) {
	// host@2 needs Node 24; plugin@2 needs host ^2. With Node 22 neither can
	// move to 2.x, but plugin can still take its latest 1.x.
	pkgs := []Package{
		planPkg(t, "host", "1.0.0", []string{"1.0.0", "2.0.0"}, map[string]string{"2.0.0": ">=24"}, nil),
		planPkg(t, "plugin", "1.0.0", []string{"1.0.0", "1.2.0", "2.0.0"}, nil, map[string]map[string]string{
			"1.0.0": {"host": "^1.0.0"},
			"1.2.0": {"host": "^1.0.0"},
			"2.0.0": {"host": "^2.0.0"},
		}),
	}
	ds := Plan(pkgs, Options{Node: mustParse(t, "22.0.0")})

	if !ds[0].HeldBack() {
		t.Errorf("host: want held back by Node, got %+v", ds[0])
	}
	if ds[1].Available != "1.2.0" || ds[1].RequiresWith != nil {
		t.Errorf("plugin: got %s requires %v, want 1.2.0 on its own", ds[1].Available, ds[1].RequiresWith)
	}
}
