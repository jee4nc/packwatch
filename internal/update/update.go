// Package update decides which version of a package to suggest, taking the
// active Node version and peer-dependency constraints into account.
package update

import (
	"fmt"
	"strings"

	"github.com/jee4nc/packwatch/internal/registry"
	"github.com/jee4nc/packwatch/internal/semver"
)

// PeerConstraint is a peerDependencies constraint that an installed package
// imposes on another package.
type PeerConstraint struct {
	Constraint string
	Source     string // package that imposes this constraint
}

// Decision is the suggested update for a single package.
type Decision struct {
	Available     string
	UpdateType    semver.UpdateType
	NodeWarning   string
	PeerWarning   string
	CompatVersion string
}

// HeldBack reports whether a newer version exists but cannot be suggested
// because of Node engine or peer-dependency constraints.
func (d Decision) HeldBack() bool {
	return d.UpdateType == semver.UpToDate && (d.NodeWarning != "" || d.PeerWarning != "")
}

// Decide picks the version to suggest for an installed package. It starts from
// the registry's latest and falls back to the newest version compatible with
// the active Node and with the peer constraints of other installed packages.
func Decide(installed semver.Version, reg registry.PackageVersions, node semver.Version, peers []PeerConstraint) Decision {
	d := Decision{
		Available:  reg.Latest.String(),
		UpdateType: semver.ClassifyUpdate(installed, reg.Latest),
	}
	if d.UpdateType == semver.UpToDate {
		return d
	}

	// Check if latest requires newer Node
	if latestEngines, ok := reg.Engines[reg.Latest.String()]; ok && !semver.SatisfiesConstraints(node, latestEngines) {
		requirement := latestEngines
		if minNode, ok := semver.ExtractMinNodeVersion(latestEngines); ok {
			requirement = "Node >=" + minNode.String()
		}
		compat, found := registry.FindCompatibleLatest(reg, node)
		switch {
		case !found:
			d.NodeWarning = fmt.Sprintf("no compatible version found for Node %s", node)
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

	// Check peer dependency constraints from other installed packages
	if len(peers) == 0 {
		return d
	}
	candidate, _ := semver.Parse(d.Available)
	var constraints, conflictSources []string
	for _, pc := range peers {
		constraints = append(constraints, pc.Constraint)
		if !semver.SatisfiesConstraints(candidate, pc.Constraint) {
			conflictSources = append(conflictSources, pc.Source)
		}
	}
	if len(conflictSources) == 0 {
		return d
	}
	sources := strings.Join(conflictSources, ", ")

	compat, found := registry.FindPeerCompatibleLatest(reg, node, constraints)
	switch {
	case !found:
		d.PeerWarning = fmt.Sprintf("no compatible version found (peer deps: %s)", sources)
		d.UpdateType = semver.UpToDate
	case compat.Compare(installed) <= 0:
		d.PeerWarning = fmt.Sprintf("latest (%s) breaks peer deps of %s; already on newest compatible version",
			d.Available, sources)
		d.UpdateType = semver.UpToDate
	default:
		d.PeerWarning = fmt.Sprintf("latest (%s) breaks peer deps of %s; suggesting %s instead",
			d.Available, sources, compat)
		d.Available = compat.String()
		d.CompatVersion = compat.String()
		d.UpdateType = semver.ClassifyUpdate(installed, compat)
	}
	return d
}
