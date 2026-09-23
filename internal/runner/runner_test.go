package runner

import (
	"reflect"
	"testing"

	"github.com/jee4nc/packwatch/internal/tui"
)

func TestGenerateCommands(t *testing.T) {
	items := []tui.Item{
		{Name: "express", Available: "4.21.0", Range: "^4.18.0"},
		{Name: "lodash", Available: "4.17.21", Range: "~4.17.0"},
		{Name: "react", Available: "18.3.1", Range: "18.2.0"},
		{Name: "chalk", Available: "5.3.0", Range: ">=4"},
		{Name: "vitest", Available: "2.1.0", Range: "^1.0.0", IsDev: true},
		{Name: "typescript", Available: "5.6.2", Range: "5.4.5", IsDev: true},
	}

	cmds := GenerateCommands(items)

	want := []Command{
		{
			Args:    []string{"install", "express@^4.21.0", "lodash@~4.17.21", "chalk@5.3.0"},
			Display: "npm install 'express@^4.21.0' 'lodash@~4.17.21' chalk@5.3.0",
		},
		{
			Args:    []string{"install", "--save-exact", "react@18.3.1"},
			Display: "npm install --save-exact react@18.3.1",
		},
		{
			Args:    []string{"install", "--save-dev", "vitest@^2.1.0"},
			Display: "npm install --save-dev 'vitest@^2.1.0'",
			IsDev:   true,
		},
		{
			Args:    []string{"install", "--save-dev", "--save-exact", "typescript@5.6.2"},
			Display: "npm install --save-dev --save-exact typescript@5.6.2",
			IsDev:   true,
		},
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("GenerateCommands() =\n%+v\nwant\n%+v", cmds, want)
	}
}

func TestInstallSpec(t *testing.T) {
	tests := []struct {
		rng       string
		wantSpec  string
		wantExact bool
	}{
		{"^1.0.0", "pkg@^2.0.0", false},
		{"~1.0.0", "pkg@~2.0.0", false},
		{"1.0.0", "pkg@2.0.0", true},
		{"=1.0.0", "pkg@2.0.0", true},
		{"1.0.0-beta.1", "pkg@2.0.0", true},
		{"1.x", "pkg@2.0.0", false},
		{">=1 <3", "pkg@2.0.0", false},
		{"", "pkg@2.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.rng, func(t *testing.T) {
			spec, exact := installSpec(tui.Item{Name: "pkg", Available: "2.0.0", Range: tt.rng})
			if spec != tt.wantSpec || exact != tt.wantExact {
				t.Errorf("installSpec(%q) = (%q, %v), want (%q, %v)", tt.rng, spec, exact, tt.wantSpec, tt.wantExact)
			}
		})
	}
}

func TestGenerateUninstallCommands(t *testing.T) {
	cmds := GenerateUninstallCommands([]tui.UnusedItem{{Name: "a"}, {Name: "@s/b"}})
	if len(cmds) != 1 || cmds[0].Display != "npm uninstall a @s/b" {
		t.Errorf("GenerateUninstallCommands() = %+v", cmds)
	}
	if cmds := GenerateUninstallCommands(nil); cmds != nil {
		t.Errorf("GenerateUninstallCommands(nil) = %+v, want nil", cmds)
	}
}
