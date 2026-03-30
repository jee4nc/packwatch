package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jee4nc/packwatch/internal/styles"
)

// VulnInfo holds a single vulnerability for display.
type VulnInfo struct {
	ID       string
	Summary  string
	Severity string
	Fixed    bool
}

// Item represents a package in the TUI.
type Item struct {
	Name          string
	Installed     string
	Available     string
	UpdateType    string // "patch", "minor", "major"
	IsDev         bool
	Selectable    bool
	Selected      bool
	NodeWarning   string
	CompatVersion string
	VulnCount     int
	VulnSeverity  string // highest severity: CRITICAL, HIGH, MEDIUM, LOW, UNKNOWN
	VulnFixed     int
	Vulns         []VulnInfo
}

// Result holds the user's selections.
type Result struct {
	Selected []Item
	Aborted  bool
}

type row struct {
	isHeader bool
	header   string
	itemIdx  int
}

type model struct {
	items         []Item
	rows          []row
	cursor        int
	viewport      int
	maxName       int
	updateCount   int
	termWidth     int
	termHeight    int
	quitting      bool
	confirmed     bool
	expandedVulns map[int]bool // itemIdx → expanded
}

func newModel(items []Item) model {
	maxName := 0
	for _, it := range items {
		if len(it.Name) > maxName {
			maxName = len(it.Name)
		}
	}
	if maxName > 40 {
		maxName = 40
	}

	var rows []row

	// Production dependencies
	hasDeps := false
	for _, it := range items {
		if !it.IsDev {
			hasDeps = true
			break
		}
	}
	if hasDeps {
		rows = append(rows, row{isHeader: true, header: "dependencies"})
		for i, it := range items {
			if !it.IsDev {
				rows = append(rows, row{itemIdx: i})
			}
		}
	}

	// Dev dependencies
	hasDevDeps := false
	for _, it := range items {
		if it.IsDev {
			hasDevDeps = true
			break
		}
	}
	if hasDevDeps {
		rows = append(rows, row{isHeader: true, header: "devDependencies"})
		for i, it := range items {
			if it.IsDev {
				rows = append(rows, row{itemIdx: i})
			}
		}
	}

	cursor := 0
	for cursor < len(rows) && rows[cursor].isHeader {
		cursor++
	}

	updateCount := 0
	for _, it := range items {
		if it.Selectable {
			updateCount++
		}
	}

	return model{
		items:         items,
		rows:          rows,
		cursor:        cursor,
		maxName:       maxName,
		updateCount:   updateCount,
		termWidth:     80,
		termHeight:    24,
		expandedVulns: make(map[int]bool),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			m.moveCursor(-1)

		case "down", "j":
			m.moveCursor(1)

		case " ", "tab":
			if m.cursor < len(m.rows) && !m.rows[m.cursor].isHeader {
				it := &m.items[m.rows[m.cursor].itemIdx]
				if it.Selectable {
					it.Selected = !it.Selected
				}
			}

		case "a":
			allSelected := true
			for _, it := range m.items {
				if it.Selectable && !it.Selected {
					allSelected = false
					break
				}
			}
			for i := range m.items {
				if m.items[i].Selectable {
					m.items[i].Selected = !allSelected
				}
			}

		case "p":
			for i := range m.items {
				if m.items[i].Selectable {
					m.items[i].Selected = strings.ToLower(m.items[i].UpdateType) == "patch"
				}
			}

		case "m":
			for i := range m.items {
				if m.items[i].Selectable {
					t := strings.ToLower(m.items[i].UpdateType)
					m.items[i].Selected = t == "patch" || t == "minor"
				}
			}

		case "v":
			if m.cursor < len(m.rows) && !m.rows[m.cursor].isHeader {
				idx := m.rows[m.cursor].itemIdx
				if m.items[idx].VulnCount > 3 {
					m.expandedVulns[idx] = !m.expandedVulns[idx]
					m.ensureVisible()
				}
			}

		case "enter":
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) moveCursor(dir int) {
	prev := m.cursor
	for {
		m.cursor += dir
		if m.cursor < 0 || m.cursor >= len(m.rows) {
			m.cursor = prev // stay in place, don't wrap
			return
		}
		if !m.rows[m.cursor].isHeader {
			break
		}
	}
	m.ensureVisible()
}

func (m *model) ensureVisible() {
	if m.cursor < m.viewport {
		m.viewport = m.cursor
	}
	for m.viewport < m.cursor {
		lines := m.countViewportLines(m.viewport, m.cursor+1)
		if lines <= m.availableLines() {
			break
		}
		m.viewport++
	}
}

const chromeLines = 10 // header(5) + selected(2) + help(3)

func (m model) availableLines() int {
	v := m.termHeight - chromeLines
	if v < 5 {
		v = 5
	}
	return v
}

func (m model) rowLines(idx int) int {
	if idx < 0 || idx >= len(m.rows) {
		return 0
	}
	r := m.rows[idx]
	if r.isHeader {
		return 1
	}
	it := m.items[r.itemIdx]
	lines := 1
	if it.NodeWarning != "" && it.Selectable {
		lines++
	}
	if it.VulnCount > 0 {
		lines++ // vuln header line
		if m.expandedVulns[r.itemIdx] {
			lines += it.VulnCount
			if it.VulnCount > 3 {
				lines++ // "▾ collapse" line
			}
		} else {
			shown := it.VulnCount
			if shown > 3 {
				shown = 3 + 1 // 3 vulns + "▸ show N more"
			}
			lines += shown
		}
	}
	return lines
}

func (m model) countViewportLines(from, to int) int {
	total := 0
	for i := from; i < to && i < len(m.rows); i++ {
		total += m.rowLines(i)
	}
	return total
}

func (m model) visibleEnd() int {
	budget := m.availableLines()
	used := 0
	for i := m.viewport; i < len(m.rows); i++ {
		cost := m.rowLines(i)
		if used+cost > budget {
			return i
		}
		used += cost
	}
	return len(m.rows)
}

func (m model) View() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString("  " + styles.BoldCyan.Render(styles.Emoji("📦 ")+"packwatch") +
		styles.Gray.Render(" — select packages to update"))
	b.WriteString("\n")
	b.WriteString("  " + styles.Gray.Render(fmt.Sprintf("%d updates available · %d packages total",
		m.updateCount, len(m.items))))
	b.WriteString("\n")
	b.WriteString("  " + styles.Gray.Render(strings.Repeat("─", 62)))
	b.WriteString("\n\n")

	// Package rows
	end := m.visibleEnd()

	for i := m.viewport; i < end; i++ {
		r := m.rows[i]

		if r.isHeader {
			b.WriteString("  " + styles.Separator(r.header, 62))
			b.WriteString("\n")
			continue
		}

		it := m.items[r.itemIdx]
		isCursor := i == m.cursor

		// Cursor
		cursor := "  "
		if isCursor {
			cursor = styles.BoldCyan.Render("▸ ")
		}

		// Checkbox
		var checkbox string
		if !it.Selectable {
			checkbox = styles.DimGreen.Render("✔ ")
		} else if it.Selected {
			checkbox = styles.BoldGreen.Render("◉ ")
		} else {
			checkbox = styles.Gray.Render("○ ")
		}

		// Name (truncated & padded)
		name := it.Name
		if len(name) > m.maxName {
			name = name[:m.maxName-3] + "..."
		}
		nameStr := fmt.Sprintf("%-*s", m.maxName, name)
		if isCursor {
			nameStr = styles.Bold.Render(nameStr)
		}

		// Version info
		var versionStr string
		if !it.Selectable {
			versionStr = styles.DimGreen.Render(it.Installed) + "  " + styles.Faint.Render("up-to-date")
		} else {
			installed := styles.Gray.Render(it.Installed)
			arrow := " " + styles.UpdateTypeArrow(it.UpdateType) + " "
			available := styles.UpdateTypeStyle(it.UpdateType).Render(it.Available)
			tag := " " + styles.UpdateTypeBadge(it.UpdateType)
			versionStr = installed + arrow + available + tag
		}

		b.WriteString(fmt.Sprintf("  %s%s%s  %s\n", cursor, checkbox, nameStr, versionStr))

		// Node warning
		if it.NodeWarning != "" && it.Selectable {
			b.WriteString(fmt.Sprintf("        %s%s\n",
				styles.Emoji("⚠️  "),
				styles.Yellow.Render(it.NodeWarning)))
		}

		// Vulnerability details
		if it.VulnCount > 0 {
			header := fmt.Sprintf("%d vuln", it.VulnCount)
			if it.VulnCount != 1 {
				header += "s"
			}
			header += " in " + it.Installed

			if it.VulnFixed > 0 && it.Selectable {
				fixLabel := fmt.Sprintf("update fixes %d/%d", it.VulnFixed, it.VulnCount)
				if it.VulnFixed == it.VulnCount {
					header += " " + styles.Gray.Render("·") + " " + styles.BoldGreen.Render(fixLabel)
				} else {
					header += " " + styles.Gray.Render("·") + " " + styles.Yellow.Render(fixLabel)
				}
			} else if it.VulnFixed == 0 && it.VulnCount > 0 && it.Selectable {
				header += " " + styles.Gray.Render("·") + " " + styles.Red.Render("update does not fix")
			}

			b.WriteString(fmt.Sprintf("        %s%s\n",
				styles.Emoji("🛡️  "),
				styles.VulnBadge(it.VulnCount, it.VulnSeverity)+" "+styles.Gray.Render(header)))

			expanded := m.expandedVulns[r.itemIdx]
			maxShow := 3
			if expanded {
				maxShow = len(it.Vulns)
			}

			for vi, v := range it.Vulns {
				if !expanded && vi >= 3 {
					remaining := it.VulnCount - 3
					prompt := fmt.Sprintf("▸ show %d more", remaining)
					if isCursor {
						b.WriteString(fmt.Sprintf("           %s\n",
							styles.BoldCyan.Render(prompt)+" "+styles.Gray.Render("(press v)")))
					} else {
						b.WriteString(fmt.Sprintf("           %s\n",
							styles.Gray.Render(prompt)))
					}
					break
				}
				if vi >= maxShow {
					break
				}
				sevStyle := styles.SeverityStyle(v.Severity)
				fixMark := ""
				if v.Fixed {
					fixMark = " " + styles.DimGreen.Render("✓ fixed")
				}
				summary := v.Summary
				if len(summary) > 52 {
					summary = summary[:49] + "..."
				}
				b.WriteString(fmt.Sprintf("           %s %s %s%s\n",
					sevStyle.Render(fmt.Sprintf("%-8s", v.Severity)),
					styles.Gray.Render(v.ID),
					styles.Faint.Render(summary),
					fixMark))
			}

			if expanded && it.VulnCount > 3 {
				prompt := "▾ collapse"
				if isCursor {
					b.WriteString(fmt.Sprintf("           %s\n",
						styles.BoldCyan.Render(prompt)+" "+styles.Gray.Render("(press v)")))
				} else {
					b.WriteString(fmt.Sprintf("           %s\n",
						styles.Gray.Render(prompt)))
				}
			}
		}
	}

	// Scroll indicators
	if m.viewport > 0 {
		b.WriteString(styles.Gray.Render("        ↑ more") + "\n")
	}
	if end < len(m.rows) {
		b.WriteString(styles.Gray.Render("        ↓ more") + "\n")
	}

	// Selected count
	selectedCount := 0
	for _, it := range m.items {
		if it.Selected {
			selectedCount++
		}
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s\n",
		styles.BoldCyan.Render(fmt.Sprintf("%d selected", selectedCount))))

	// Help bar
	b.WriteString("\n")
	help := []string{
		styles.Bold.Render("↑↓") + " " + styles.Gray.Render("navigate"),
		styles.Bold.Render("space") + " " + styles.Gray.Render("toggle"),
		styles.Bold.Render("a") + " " + styles.Gray.Render("all"),
		styles.Bold.Render("p") + " " + styles.Gray.Render("patch"),
		styles.Bold.Render("m") + " " + styles.Gray.Render("minor"),
		styles.Bold.Render("v") + " " + styles.Gray.Render("vulns"),
		styles.Bold.Render("enter") + " " + styles.Gray.Render("confirm"),
		styles.Bold.Render("q") + " " + styles.Gray.Render("quit"),
	}
	b.WriteString("  " + strings.Join(help, styles.Gray.Render("  ·  ")))
	b.WriteString("\n")

	return b.String()
}

// Run launches the interactive selector and returns the user's choices.
func Run(items []Item) Result {
	m := newModel(items)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return Result{Aborted: true}
	}
	fm := result.(model)
	if fm.quitting {
		return Result{Aborted: true}
	}
	var selected []Item
	for _, it := range fm.items {
		if it.Selected {
			selected = append(selected, it)
		}
	}
	return Result{Selected: selected}
}
