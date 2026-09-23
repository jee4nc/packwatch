package tui

import (
	"slices"
	"testing"
)

func groupItems() []Item {
	return []Item{
		{Name: "vitest", Selectable: true, RequiresWith: []string{"@vitest/coverage-v8"}},
		{Name: "@vitest/coverage-v8", Selectable: true, RequiresWith: []string{"vitest"}},
		{Name: "plugin", Selectable: true, RequiresWith: []string{"vitest"}},
		{Name: "lodash", Selectable: true},
	}
}

func selectedNames(items []Item) []string {
	var names []string
	for _, it := range items {
		if it.Selected {
			names = append(names, it.Name)
		}
	}
	return names
}

func TestSetSelectedCascades(t *testing.T) {
	m := model{items: groupItems()}

	m.setSelected(2, true) // plugin → vitest → coverage
	if got := selectedNames(m.items); !slices.Equal(got, []string{"vitest", "@vitest/coverage-v8", "plugin"}) {
		t.Errorf("after selecting plugin: %v", got)
	}

	m.setSelected(3, true)
	m.setSelected(1, false) // coverage → vitest (requires it) → plugin (requires vitest)
	if got := selectedNames(m.items); !slices.Equal(got, []string{"lodash"}) {
		t.Errorf("after deselecting coverage: %v", got)
	}
}

func TestWithRequired(t *testing.T) {
	all := groupItems()
	selected, added := WithRequired([]Item{all[2], all[3]}, all)

	var names []string
	for _, it := range selected {
		names = append(names, it.Name)
	}
	if !slices.Equal(names, []string{"plugin", "lodash", "vitest", "@vitest/coverage-v8"}) {
		t.Errorf("selected = %v", names)
	}
	if !slices.Equal(added, []string{"vitest", "@vitest/coverage-v8"}) {
		t.Errorf("added = %v", added)
	}

	if _, added := WithRequired([]Item{all[3]}, all); added != nil {
		t.Errorf("independent selection added %v", added)
	}
}
