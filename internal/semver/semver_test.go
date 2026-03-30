package semver

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Version
		wantErr bool
	}{
		{"1.2.3", Version{Major: 1, Minor: 2, Patch: 3, Raw: "1.2.3"}, false},
		{"v1.2.3", Version{Major: 1, Minor: 2, Patch: 3, Raw: "v1.2.3"}, false},
		{"0.0.0", Version{Major: 0, Minor: 0, Patch: 0, Raw: "0.0.0"}, false},
		{"1.2.3-beta.1", Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "beta.1", Raw: "1.2.3-beta.1"}, false},
		{"1.2.3-alpha+build", Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "alpha", Raw: "1.2.3-alpha+build"}, false},
		{"1.2", Version{Major: 1, Minor: 2, Patch: 0, Raw: "1.2"}, false},
		{"1", Version{Major: 1, Minor: 0, Patch: 0, Raw: "1"}, false},
		{"=1.2.3", Version{Major: 1, Minor: 2, Patch: 3, Raw: "=1.2.3"}, false},
		{"abc", Version{}, true},
		{"1.2.3.4", Version{}, true},
		{"", Version{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, _ := Parse(tt.a)
			b, _ := Parse(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestClassifyUpdate(t *testing.T) {
	tests := []struct {
		installed, latest string
		want              UpdateType
	}{
		{"1.0.0", "1.0.0", UpToDate},
		{"2.0.0", "1.0.0", UpToDate},
		{"1.0.0", "1.0.1", Patch},
		{"1.0.0", "1.1.0", Minor},
		{"1.0.0", "2.0.0", Major},
		{"1.2.3", "1.2.4", Patch},
		{"1.2.3", "1.3.0", Minor},
		{"1.2.3", "2.0.0", Major},
	}

	for _, tt := range tests {
		t.Run(tt.installed+"_to_"+tt.latest, func(t *testing.T) {
			inst, _ := Parse(tt.installed)
			lat, _ := Parse(tt.latest)
			got := ClassifyUpdate(inst, lat)
			if got != tt.want {
				t.Errorf("ClassifyUpdate(%s, %s) = %v, want %v", tt.installed, tt.latest, got, tt.want)
			}
		})
	}
}

func TestSatisfiesConstraints(t *testing.T) {
	tests := []struct {
		version    string
		constraint string
		want       bool
	}{
		// Basic operators
		{"16.0.0", ">=14.0.0", true},
		{"12.0.0", ">=14.0.0", false},
		{"14.0.0", ">=14.0.0", true},
		{"20.0.0", "<20.0.0", false},
		{"19.9.9", "<20.0.0", true},

		// AND groups (space-separated)
		{"16.0.0", ">=14 <20", true},
		{"22.0.0", ">=14 <20", false},
		{"10.0.0", ">=14 <20", false},

		// OR groups
		{"14.0.0", ">=14 <16 || >=18", true},
		{"15.0.0", ">=14 <16 || >=18", true},
		{"17.0.0", ">=14 <16 || >=18", false},
		{"20.0.0", ">=14 <16 || >=18", true},

		// Wildcards and empty
		{"1.0.0", "*", true},
		{"1.0.0", "", true},

		// Tilde ranges
		{"1.2.3", "~1.2.3", true},
		{"1.2.9", "~1.2.3", true},
		{"1.3.0", "~1.2.3", false},
		{"1.2.2", "~1.2.3", false},
		{"16.0.0", "~16.0.0", true},
		{"16.0.5", "~16.0.0", true},
		{"16.1.0", "~16.0.0", false},
		{"17.0.0", "~16.0.0", false},

		// Caret ranges
		{"1.2.3", "^1.2.3", true},
		{"1.9.9", "^1.2.3", true},
		{"2.0.0", "^1.2.3", false},
		{"1.2.2", "^1.2.3", false},
		{"16.0.0", "^16", true},
		{"16.20.0", "^16", true},
		{"17.0.0", "^16", false},
		{"15.0.0", "^16", false},

		// Caret with 0.x (minor-level range)
		{"0.2.3", "^0.2.3", true},
		{"0.2.9", "^0.2.3", true},
		{"0.3.0", "^0.2.3", false},
		{"0.2.2", "^0.2.3", false},

		// Caret with 0.0.x (patch-level range)
		{"0.0.3", "^0.0.3", true},
		{"0.0.4", "^0.0.3", false},
		{"0.0.2", "^0.0.3", false},

		// Real-world engines.node patterns
		{"18.0.0", "^18.0.0 || ^20.0.0 || >=22.0.0", true},
		{"20.5.0", "^18.0.0 || ^20.0.0 || >=22.0.0", true},
		{"22.1.0", "^18.0.0 || ^20.0.0 || >=22.0.0", true},
		{"19.0.0", "^18.0.0 || ^20.0.0 || >=22.0.0", false},
		{"17.0.0", "^18.0.0 || ^20.0.0 || >=22.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.version+"_satisfies_"+tt.constraint, func(t *testing.T) {
			v, err := Parse(tt.version)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.version, err)
			}
			got := SatisfiesConstraints(v, tt.constraint)
			if got != tt.want {
				t.Errorf("SatisfiesConstraints(%s, %q) = %v, want %v",
					tt.version, tt.constraint, got, tt.want)
			}
		})
	}
}

func TestExpandTilde(t *testing.T) {
	tests := []struct {
		input   string
		wantGte string
		wantLt  string
	}{
		{"1.2.3", "1.2.3", "1.3.0"},
		{"0.2.0", "0.2.0", "0.3.0"},
		{"16.0.0", "16.0.0", "16.1.0"},
	}

	for _, tt := range tests {
		t.Run("~"+tt.input, func(t *testing.T) {
			v, _ := Parse(tt.input)
			constraints := expandTilde(v)
			if len(constraints) != 2 {
				t.Fatalf("expandTilde(%s) returned %d constraints, want 2", tt.input, len(constraints))
			}
			if constraints[0].Op != ">=" || constraints[0].Version.String() != tt.wantGte {
				t.Errorf("expandTilde(%s)[0] = %s %s, want >= %s",
					tt.input, constraints[0].Op, constraints[0].Version.String(), tt.wantGte)
			}
			if constraints[1].Op != "<" || constraints[1].Version.String() != tt.wantLt {
				t.Errorf("expandTilde(%s)[1] = %s %s, want < %s",
					tt.input, constraints[1].Op, constraints[1].Version.String(), tt.wantLt)
			}
		})
	}
}

func TestExpandCaret(t *testing.T) {
	tests := []struct {
		input   string
		wantGte string
		wantLt  string
	}{
		{"1.2.3", "1.2.3", "2.0.0"},
		{"0.2.3", "0.2.3", "0.3.0"},
		{"0.0.3", "0.0.3", "0.0.4"},
		{"16.0.0", "16.0.0", "17.0.0"},
		{"0.0.0", "0.0.0", "0.0.1"},
	}

	for _, tt := range tests {
		t.Run("^"+tt.input, func(t *testing.T) {
			v, _ := Parse(tt.input)
			constraints := expandCaret(v)
			if len(constraints) != 2 {
				t.Fatalf("expandCaret(%s) returned %d constraints, want 2", tt.input, len(constraints))
			}
			if constraints[0].Op != ">=" || constraints[0].Version.String() != tt.wantGte {
				t.Errorf("expandCaret(%s)[0] = %s %s, want >= %s",
					tt.input, constraints[0].Op, constraints[0].Version.String(), tt.wantGte)
			}
			if constraints[1].Op != "<" || constraints[1].Version.String() != tt.wantLt {
				t.Errorf("expandCaret(%s)[1] = %s %s, want < %s",
					tt.input, constraints[1].Op, constraints[1].Version.String(), tt.wantLt)
			}
		})
	}
}

func TestExtractMinNodeVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantOk  bool
	}{
		{">=14.0.0", "14.0.0", true},
		{">=16 || >=18", "16.0.0", true},
		{">=18.0.0 || >=20.0.0", "18.0.0", true},
		{"*", "", false},
		{"", "", false},
		{"^16.0.0", "16.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ExtractMinNodeVersion(tt.input)
			if ok != tt.wantOk {
				t.Errorf("ExtractMinNodeVersion(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
				return
			}
			if ok && got.String() != tt.want {
				t.Errorf("ExtractMinNodeVersion(%q) = %s, want %s", tt.input, got.String(), tt.want)
			}
		})
	}
}
