// Package update decides which version of a package to suggest, taking the
// active Node version and peer-dependency constraints into account.
package update

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jee4nc/packwatch/internal/registry"
	"github.com/jee4nc/packwatch/internal/semver"
)

// maxRounds bounds the fixed-point iteration in Plan.
const maxRounds = 10

// PeerConstraint is a peerDependencies constraint that an installed package
// imposes on another package.
type PeerConstraint struct {
	Constraint string
	Source     string // package that imposes this constraint
}

// Options holds project-wide inputs to the decision.
type Options struct {
	Node semver.Version
}

// Env describes the other direct dependencies a package must be compatible with.
type Env struct {
	Peers  []PeerConstraint          // constraints other packages impose on this one
	Others map[string]semver.Version // versions the other direct deps will have
}

// Decision is the suggested update for a single package.
type Decision struct {
	Available     string
	UpdateType    semver.UpdateType
	NodeWarning   string
	PeerWarning   string
	CompatVersion string
	// RequiresWith lists packages that must be updated together with this one
	// because their peer dependencies pin each other.
	RequiresWith []string
}

// HeldBack reports whether a newer version exists but cannot be suggested
// because of Node engine or peer-dependency constraints.
func (d Decision) HeldBack() bool {
	return d.UpdateType == semver.UpToDate && (d.NodeWarning != "" || d.PeerWarning != "")
}

// target returns the version the package ends up at under this decision.
func (d Decision) target(installed semver.Version) semver.Version {
	if d.UpdateType == semver.UpToDate {
		return installed
	}
	v, err := semver.Parse(d.Available)
	if err != nil {
		return installed
	}
	return v
}

// Decide picks the version to suggest for an installed package. It starts from
// the registry's latest and falls back to the newest version compatible with
// the active Node and with the peer dependencies of the other packages in env
// (in both directions).
func Decide(installed semver.Version, reg registry.PackageVersions, opts Options, env Env) Decision {
	return decide(installed, reg, opts, env)
}

func decide(installed semver.Version, reg registry.PackageVersions, opts Options, env Env) Decision {
	d := Decision{
		Available:  reg.Latest.String(),
		UpdateType: semver.ClassifyUpdate(installed, reg.Latest),
	}
	if d.UpdateType == semver.UpToDate {
		return d
	}

	// Check if latest requires newer Node
	if latestEngines, ok := reg.Engines[reg.Latest.String()]; ok && !semver.SatisfiesConstraints(opts.Node, latestEngines) {
		requirement := latestEngines
		if minNode, ok := semver.ExtractMinNodeVersion(latestEngines); ok {
			requirement = "Node >=" + minNode.String()
		}
		compat, found := registry.FindCompatibleLatest(reg, opts.Node)
		switch {
		case !found:
			d.NodeWarning = fmt.Sprintf("no compatible version found for Node %s", opts.Node)
			d.UpdateType = semver.UpToDate
			return d
		case compat.Compare(installed) <= 0:
			d.NodeWarning = fmt.Sprintf("latest (%s) requires %s; already on newest compatible version",
				reg.Latest, requirement)
			d.UpdateType = semver.UpToDate
			return d
		default:
			d.NodeWarning = fmt.Sprintf("latest (%s) requires %s; suggesting %s instead",
				reg.Latest, requirement, compat)
			d.Available = compat.String()
			d.CompatVersion = compat.String()
			d.UpdateType = semver.ClassifyUpdate(installed, compat)
		}
	}

	// Check peer dependency constraints against the other packages
	candidate, _ := semver.Parse(d.Available)
	conflicts := peerConflicts(reg, candidate, env)
	if len(conflicts) == 0 {
		return d
	}
	sources := strings.Join(conflicts, ", ")

	compat, found := registry.FindLatest(reg, func(v semver.Version) bool {
		return registry.NodeCompatible(reg, v, opts.Node) && len(peerConflicts(reg, v, env)) == 0
	})
	switch {
	case !found:
		d.PeerWarning = fmt.Sprintf("no compatible version found (peer deps: %s)", sources)
		d.UpdateType = semver.UpToDate
	case compat.Compare(installed) <= 0:
		d.PeerWarning = fmt.Sprintf("latest (%s) conflicts with peer deps of %s; already on newest compatible version",
			d.Available, sources)
		d.UpdateType = semver.UpToDate
	default:
		d.PeerWarning = fmt.Sprintf("latest (%s) conflicts with peer deps of %s; suggesting %s instead",
			d.Available, sources, compat)
		d.Available = compat.String()
		d.CompatVersion = compat.String()
		d.UpdateType = semver.ClassifyUpdate(installed, compat)
	}
	return d
}

// peerConflicts returns the packages whose peer relation with version v of
// this package is broken: constraints they impose on it, and constraints v
// imposes on them.
func peerConflicts(reg registry.PackageVersions, v semver.Version, env Env) []string {
	set := map[string]bool{}
	for _, pc := range env.Peers {
		if !semver.SatisfiesConstraints(v, pc.Constraint) {
			set[pc.Source] = true
		}
	}
	for dep, constraint := range reg.PeerDeps[v.String()] {
		if other, ok := env.Others[dep]; ok && !semver.SatisfiesConstraints(other, constraint) {
			set[dep] = true
		}
	}
	return sortedKeys(set)
}

// Package is a direct dependency with its registry metadata.
type Package struct {
	Name      string
	Installed semver.Version
	Registry  registry.PackageVersions
}

// Plan decides all packages together, so packages whose peer dependencies pin
// each other (e.g. vitest and @vitest/coverage-v8) can be updated as a group.
//
// It starts from every package at its best Node-compatible version and
// re-decides each package against the others' resulting versions until
// nothing changes. If that doesn't settle, it falls back to deciding each
// package against the installed versions only.
func Plan(pkgs []Package, opts Options) []Decision {
	installed := make(map[string]semver.Version, len(pkgs))
	targets := make(map[string]semver.Version, len(pkgs))
	for _, p := range pkgs {
		installed[p.Name] = p.Installed
		targets[p.Name] = Decide(p.Installed, p.Registry, opts, Env{}).target(p.Installed)
	}

	for round := 0; round < maxRounds; round++ {
		decisions, next := decideAll(pkgs, opts, targets)
		if sameVersions(next, targets) {
			addRequirements(decisions, pkgs, installed, next)
			return decisions
		}
		targets = next
	}

	decisions, _ := decideAll(pkgs, opts, installed)
	return decisions
}

func decideAll(pkgs []Package, opts Options, targets map[string]semver.Version) ([]Decision, map[string]semver.Version) {
	decisions := make([]Decision, len(pkgs))
	next := make(map[string]semver.Version, len(pkgs))
	for i, p := range pkgs {
		decisions[i] = Decide(p.Installed, p.Registry, opts, envFor(p.Name, pkgs, targets))
		next[p.Name] = decisions[i].target(p.Installed)
	}
	return decisions, next
}

// envFor builds the environment of one package given the versions of all others.
func envFor(name string, pkgs []Package, targets map[string]semver.Version) Env {
	env := Env{Others: make(map[string]semver.Version, len(pkgs))}
	for _, p := range pkgs {
		if p.Name == name {
			continue
		}
		v := targets[p.Name]
		env.Others[p.Name] = v
		if c, ok := p.Registry.PeerDeps[v.String()][name]; ok {
			env.Peers = append(env.Peers, PeerConstraint{Constraint: c, Source: p.Name})
		}
	}
	return env
}

// addRequirements records, for each updated package, the other updated
// packages it is not compatible with at their installed versions.
func addRequirements(decisions []Decision, pkgs []Package, installed, targets map[string]semver.Version) {
	for i, p := range pkgs {
		if decisions[i].UpdateType == semver.UpToDate {
			continue
		}
		pTarget := targets[p.Name]
		set := map[string]bool{}
		for _, o := range pkgs {
			if o.Name == p.Name || targets[o.Name].Compare(installed[o.Name]) == 0 {
				continue
			}
			oInstalled := installed[o.Name]
			if c, ok := o.Registry.PeerDeps[oInstalled.String()][p.Name]; ok && !semver.SatisfiesConstraints(pTarget, c) {
				set[o.Name] = true
			}
			if c, ok := p.Registry.PeerDeps[pTarget.String()][o.Name]; ok && !semver.SatisfiesConstraints(oInstalled, c) {
				set[o.Name] = true
			}
		}
		decisions[i].RequiresWith = sortedKeys(set)
	}
}

func sameVersions(a, b map[string]semver.Version) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v.Compare(w) != 0 {
			return false
		}
	}
	return true
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
