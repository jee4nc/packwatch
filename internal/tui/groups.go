package tui

// WithRequired returns the selection plus every item it requires, directly or
// transitively, through RequiresWith, and the names that had to be added.
func WithRequired(selected, all []Item) ([]Item, []string) {
	byName := make(map[string]Item, len(all))
	for _, it := range all {
		byName[it.Name] = it
	}
	in := make(map[string]bool, len(selected))
	queue := make([]string, 0, len(selected))
	for _, it := range selected {
		in[it.Name] = true
		queue = append(queue, it.Name)
	}

	result := append([]Item(nil), selected...)
	var added []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, req := range byName[name].RequiresWith {
			it, ok := byName[req]
			if in[req] || !ok || !it.Selectable {
				continue
			}
			in[req] = true
			it.Selected = true
			result = append(result, it)
			added = append(added, req)
			queue = append(queue, req)
		}
	}
	return result, added
}

// setSelected changes an item's selection and keeps groups consistent:
// selecting an item selects everything it requires; deselecting it deselects
// everything that requires it.
func (m *model) setSelected(idx int, selected bool) {
	if !m.items[idx].Selectable || m.items[idx].Selected == selected {
		return
	}
	m.items[idx].Selected = selected
	name := m.items[idx].Name

	for i := range m.items {
		if selected && contains(m.items[idx].RequiresWith, m.items[i].Name) {
			m.setSelected(i, true)
		}
		if !selected && contains(m.items[i].RequiresWith, name) {
			m.setSelected(i, false)
		}
	}
}

// selectRequired extends the current selection with the items it requires.
func (m *model) selectRequired() {
	for i := range m.items {
		if m.items[i].Selected {
			for j := range m.items {
				if contains(m.items[i].RequiresWith, m.items[j].Name) {
					m.setSelected(j, true)
				}
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
