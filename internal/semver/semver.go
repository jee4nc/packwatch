// Package semver provides semantic versioning parsing, comparison,
// and constraint checking using only the Go standard library.
package semver

import (
	"fmt"
	"regexp"
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

// Constraint represents a single comparator like ">=14.0.0" or "<20.0.0".
// Ranges (x-ranges, ~, ^, hyphen ranges, partial versions) are expanded
// into one or more comparators by parseConstraints.
type Constraint struct {
	Op      string // ">=", "<=", ">", "<", "="
	Version Version
}

// never is a comparator that no version satisfies (e.g. "<*" or ">*").
var never = Constraint{Op: "<", Version: Version{}}

// opSpaceRe matches an operator followed by whitespace (">= 14"), so the
// operator can be rejoined to its version before tokenizing.
var opSpaceRe = regexp.MustCompile(`(>=|<=|~>|>|<|=|~|\^)\s+`)

// SatisfiesConstraints checks if a version satisfies a range string using
// node-semver semantics: ">=14.0.0", ">=14 <20", "^18 || ^20", "16.x",
// "1.2.3 - 2.3", "~1.2", "18.2.0" (exact).
//
// A range that cannot be parsed (e.g. "latest", "npm:foo@1") is treated as
// satisfied: packwatch cannot evaluate it, so it must not block updates.
func SatisfiesConstraints(v Version, rangeStr string) bool {
	groups, err := parseRange(rangeStr)
	if err != nil {
		return true
	}
	for _, group := range groups {
		if satisfiesAll(v, group) {
			return true
		}
	}
	return false
}

func satisfiesAll(v Version, constraints []Constraint) bool {
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
	case "=":
		return cmp == 0
	}
	return false
}

// parseRange splits a range on "||" and parses each AND group.
func parseRange(s string) ([][]Constraint, error) {
	var groups [][]Constraint
	for _, group := range strings.Split(s, "||") {
		constraints, err := parseConstraints(group)
		if err != nil {
			return nil, err
		}
		groups = append(groups, constraints)
	}
	return groups, nil
}

// parseConstraints parses a space-separated AND group into comparators.
// An empty group (or "*") yields no comparators, which matches any version.
func parseConstraints(s string) ([]Constraint, error) {
	s = opSpaceRe.ReplaceAllString(strings.TrimSpace(s), "$1")
	tokens := strings.Fields(s)

	var constraints []Constraint
	for i := 0; i < len(tokens); i++ {
		// Hyphen range: "a - b"
		if i+2 < len(tokens) && tokens[i+1] == "-" {
			lo, err := parsePartial(tokens[i])
			if err != nil {
				return nil, err
			}
			hi, err := parsePartial(tokens[i+2])
			if err != nil {
				return nil, err
			}
			constraints = append(constraints, expand(">=", lo)...)
			constraints = append(constraints, expand("<=", hi)...)
			i += 2
			continue
		}

		tok := tokens[i]
		op := ""
		for _, prefix := range []string{">=", "<=", "~>", ">", "<", "=", "~", "^"} {
			if strings.HasPrefix(tok, prefix) {
				op = prefix
				tok = strings.TrimPrefix(tok, prefix)
				break
			}
		}
		if op == "~>" {
			op = "~"
		}
		p, err := parsePartial(tok)
		if err != nil {
			return nil, err
		}
		constraints = append(constraints, expand(op, p)...)
	}
	return constraints, nil
}

// partial is a possibly incomplete version: "1", "1.2", "1.x", "*".
// n is the number of numeric components given (0 means wildcard).
type partial struct {
	v Version
	n int
}

func parsePartial(s string) (partial, error) {
	raw := s
	s = strings.TrimPrefix(s, "=")
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return partial{}, fmt.Errorf("invalid version in range: %q", raw)
	}

	var pre string
	if idx := strings.Index(s, "+"); idx != -1 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "-"); idx != -1 {
		pre = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return partial{}, fmt.Errorf("invalid version in range: %q", raw)
	}

	var nums [3]int
	n := 0
	for i, part := range parts {
		if part == "x" || part == "X" || part == "*" {
			break
		}
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 {
			return partial{}, fmt.Errorf("invalid version in range: %q", raw)
		}
		nums[i] = num
		n = i + 1
	}

	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Raw: raw}
	if n == 3 {
		v.Prerelease = pre
	}
	return partial{v: v, n: n}, nil
}

// bump returns the first version outside the partial's x-range:
// "1" → 2.0.0, "1.2" → 1.3.0.
func bump(p partial) Version {
	if p.n == 1 {
		return Version{Major: p.v.Major + 1}
	}
	return Version{Major: p.v.Major, Minor: p.v.Minor + 1}
}

// expand turns an operator and a (possibly partial) version into comparators.
func expand(op string, p partial) []Constraint {
	if p.n == 0 {
		if op == "<" || op == ">" {
			return []Constraint{never}
		}
		return nil // matches anything
	}

	switch op {
	case "~":
		return expandTilde(p)
	case "^":
		return expandCaret(p)
	case ">=":
		return []Constraint{{Op: ">=", Version: p.v}}
	case "<":
		return []Constraint{{Op: "<", Version: p.v}}
	case ">":
		if p.n == 3 {
			return []Constraint{{Op: ">", Version: p.v}}
		}
		return []Constraint{{Op: ">=", Version: bump(p)}}
	case "<=":
		if p.n == 3 {
			return []Constraint{{Op: "<=", Version: p.v}}
		}
		return []Constraint{{Op: "<", Version: bump(p)}}
	default: // "" or "="
		if p.n == 3 {
			return []Constraint{{Op: "=", Version: p.v}}
		}
		return []Constraint{{Op: ">=", Version: p.v}, {Op: "<", Version: bump(p)}}
	}
}

// expandTilde expands ~ to a [>=version, <upper) range.
//
//	~1.2.3 → >=1.2.3 <1.3.0
//	~1.2   → >=1.2.0 <1.3.0
//	~1     → >=1.0.0 <2.0.0
func expandTilde(p partial) []Constraint {
	upper := Version{Major: p.v.Major, Minor: p.v.Minor + 1}
	if p.n == 1 {
		upper = Version{Major: p.v.Major + 1}
	}
	return []Constraint{
		{Op: ">=", Version: p.v},
		{Op: "<", Version: upper},
	}
}

// expandCaret expands ^ to a range compatible with the leftmost non-zero component.
//
//	^1.2.3 → >=1.2.3 <2.0.0
//	^0.2.3 → >=0.2.3 <0.3.0
//	^0.0.3 → >=0.0.3 <0.0.4
//	^0.0   → >=0.0.0 <0.1.0
//	^0     → >=0.0.0 <1.0.0
func expandCaret(p partial) []Constraint {
	var upper Version
	switch {
	case p.v.Major > 0 || p.n == 1:
		upper = Version{Major: p.v.Major + 1}
	case p.v.Minor > 0 || p.n == 2:
		upper = Version{Minor: p.v.Minor + 1}
	default:
		upper = Version{Patch: p.v.Patch + 1}
	}
	return []Constraint{
		{Op: ">=", Version: p.v},
		{Op: "<", Version: upper},
	}
}

// ExtractMinNodeVersion extracts the minimum required Node version from an engines string.
// Returns zero version and false if no minimum can be determined.
func ExtractMinNodeVersion(enginesStr string) (Version, bool) {
	groups, err := parseRange(enginesStr)
	if err != nil {
		return Version{}, false
	}

	// Find the lowest lower bound across all OR groups
	var minVer Version
	found := false
	for _, group := range groups {
		for _, c := range group {
			if c.Op == ">=" || c.Op == ">" || c.Op == "=" {
				if !found || c.Version.LessThan(minVer) {
					minVer = c.Version
					found = true
				}
			}
		}
	}
	return minVer, found
}
