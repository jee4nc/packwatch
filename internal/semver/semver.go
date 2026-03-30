// Package semver provides semantic versioning parsing, comparison,
// and constraint checking using only the Go standard library.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version represents a parsed semantic version.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Raw        string
}

// UpdateType classifies the kind of update between two versions.
type UpdateType int

const (
	UpToDate UpdateType = iota
	Patch
	Minor
	Major
)

func (u UpdateType) String() string {
	switch u {
	case Patch:
		return "patch"
	case Minor:
		return "minor"
	case Major:
		return "major"
	default:
		return "up-to-date"
	}
}

// Parse parses a semver string like "1.2.3", "v1.2.3", or "1.2.3-beta.1".
func Parse(s string) (Version, error) {
	raw := s
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "=")
	s = strings.TrimSpace(s)

	var pre string
	if idx := strings.Index(s, "-"); idx != -1 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	// Strip build metadata
	if idx := strings.Index(pre, "+"); idx != -1 {
		pre = pre[:idx]
	} else if idx := strings.Index(s, "+"); idx != -1 {
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return Version{}, fmt.Errorf("invalid semver: %s", raw)
	}

	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, fmt.Errorf("invalid semver component %q in %s", p, raw)
		}
		nums[i] = n
	}

	return Version{
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		Prerelease: pre,
		Raw:        raw,
	}, nil
}

// String returns the canonical semver string.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	return s
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other.
func (v Version) Compare(other Version) int {
	if c := cmpInt(v.Major, other.Major); c != 0 {
		return c
	}
	if c := cmpInt(v.Minor, other.Minor); c != 0 {
		return c
	}
	if c := cmpInt(v.Patch, other.Patch); c != 0 {
		return c
	}
	return comparePrerelease(v.Prerelease, other.Prerelease)
}

// LessThan returns true if v < other.
func (v Version) LessThan(other Version) bool {
	return v.Compare(other) < 0
}

// ClassifyUpdate returns the update type from installed to latest.
func ClassifyUpdate(installed, latest Version) UpdateType {
	if installed.Compare(latest) >= 0 {
		return UpToDate
	}
	if latest.Major > installed.Major {
		return Major
	}
	if latest.Minor > installed.Minor {
		return Minor
	}
	return Patch
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	// No prerelease > has prerelease (1.0.0 > 1.0.0-alpha)
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		if aErr == nil && bErr == nil {
			if c := cmpInt(aNum, bNum); c != 0 {
				return c
			}
		} else {
			if c := strings.Compare(aParts[i], bParts[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(aParts), len(bParts))
}

// Constraint represents a single node engine constraint like ">=14.0.0", "<20", ">=16".
type Constraint struct {
	Op      string // ">=", "<=", ">", "<", "=", ""
	Version Version
}

// SatisfiesConstraints checks if a version satisfies a node engines string
// like ">=14.0.0", ">=14 <20", ">=16.0.0 || >=18.0.0".
func SatisfiesConstraints(v Version, enginesStr string) bool {
	enginesStr = strings.TrimSpace(enginesStr)
	if enginesStr == "" || enginesStr == "*" {
		return true
	}

	// Handle OR groups: ">=14 || >=16"
	orGroups := strings.Split(enginesStr, "||")
	for _, group := range orGroups {
		if satisfiesAndGroup(v, strings.TrimSpace(group)) {
			return true
		}
	}
	return false
}

func satisfiesAndGroup(v Version, group string) bool {
	constraints := parseConstraints(group)
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if !satisfiesSingle(v, c) {
			return false
		}
	}
	return true
}

func satisfiesSingle(v Version, c Constraint) bool {
	cmp := v.Compare(c.Version)
	switch c.Op {
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "=", "":
		return cmp == 0
	}
	return false
}

func parseConstraints(s string) []Constraint {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	var constraints []Constraint
	tokens := strings.Fields(s)
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" || tok == "-" {
			continue
		}
		var op string
		for _, prefix := range []string{">=", "<=", ">", "<", "=", "~", "^"} {
			if strings.HasPrefix(tok, prefix) {
				op = prefix
				tok = strings.TrimPrefix(tok, prefix)
				break
			}
		}
		v, err := Parse(tok)
		if err != nil {
			continue
		}
		if op == "~" {
			constraints = append(constraints, expandTilde(v)...)
			continue
		}
		if op == "^" {
			constraints = append(constraints, expandCaret(v)...)
			continue
		}
		if op == "" {
			op = ">="
		}
		constraints = append(constraints, Constraint{Op: op, Version: v})
	}
	return constraints
}

// expandTilde expands ~ to a [>=version, <next-minor) range.
//
//	~1.2.3 → >=1.2.3 <1.3.0
//	~0.2   → >=0.2.0 <0.3.0
//	~1     → >=1.0.0 <2.0.0
func expandTilde(v Version) []Constraint {
	upper := Version{Major: v.Major, Minor: v.Minor + 1, Patch: 0}
	return []Constraint{
		{Op: ">=", Version: v},
		{Op: "<", Version: upper},
	}
}

// expandCaret expands ^ to a range compatible with the leftmost non-zero component.
//
//	^1.2.3 → >=1.2.3 <2.0.0   (major > 0: next major)
//	^0.2.3 → >=0.2.3 <0.3.0   (major == 0, minor > 0: next minor)
//	^0.0.3 → >=0.0.3 <0.0.4   (major == 0, minor == 0: next patch)
func expandCaret(v Version) []Constraint {
	var upper Version
	switch {
	case v.Major > 0:
		upper = Version{Major: v.Major + 1}
	case v.Minor > 0:
		upper = Version{Minor: v.Minor + 1}
	default:
		upper = Version{Patch: v.Patch + 1}
	}
	return []Constraint{
		{Op: ">=", Version: v},
		{Op: "<", Version: upper},
	}
}

// ExtractMinNodeVersion extracts the minimum required Node version from an engines string.
// Returns zero version and false if no minimum can be determined.
func ExtractMinNodeVersion(enginesStr string) (Version, bool) {
	enginesStr = strings.TrimSpace(enginesStr)
	if enginesStr == "" || enginesStr == "*" {
		return Version{}, false
	}

	// Find the lowest >= constraint across all OR groups
	var minVer Version
	found := false

	orGroups := strings.Split(enginesStr, "||")
	for _, group := range orGroups {
		constraints := parseConstraints(strings.TrimSpace(group))
		for _, c := range constraints {
			if c.Op == ">=" || c.Op == ">" || c.Op == "=" || c.Op == "" {
				if !found || c.Version.LessThan(minVer) {
					minVer = c.Version
					found = true
				}
			}
		}
	}
	return minVer, found
}
