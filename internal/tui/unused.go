package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jee4nc/packwatch/internal/styles"
)

// UnusedItem represents an unused package for display.
type UnusedItem struct {
	Name    string
	Version string
}

type unusedModel struct {
	items      []UnusedItem
	cursor     int
	viewport   int
	maxName    int
	termWidth  int
	termHeight int
	quitting   bool
	total      int
	scanned    int
}

func newUnusedModel(items []UnusedItem, total, scanned int) unusedModel {
	maxName := 0
	for _, it := range items {
		if len(it.Name) > maxName {
			maxName = len(it.Name)
		}
	}
	if maxName > 40 {
		maxName = 40
	}

	return unusedModel{
		items:      items,
		maxName:    maxName,
		termWidth:  80,
		termHeight: 24,
		total:      total,
		scanned:    scanned,
	}
}

func (m unusedModel) Init() tea.Cmd {
	return nil
}

func (m unusedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.viewport {
					m.viewport = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.viewport+m.availableLines() {
					m.viewport = m.cursor - m.availableLines() + 1
				}
			}
		}
	}
	return m, nil
}

const unusedChromeLines = 9 // header(5) + summary(1) + spacing(1) + help(2)

func (m unusedModel) availableLines() int {
	return max(m.termHeight-unusedChromeLines, 5)
}

func (m unusedModel) View() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString("  " + styles.BoldCyan.Render(styles.Emoji("📦 ")+"packwatch") +
		styles.Gray.Render(" — unused dependencies"))
	b.WriteString("\n")
	b.WriteString("  " + styles.Gray.Render(fmt.Sprintf("%d unused out of %d total · %d files scanned",
		len(m.items), m.total, m.scanned)))
	b.WriteString("\n")
	b.WriteString("  " + styles.Gray.Render(strings.Repeat("─", 62)))
	b.WriteString("\n\n")

	// Items
	avail := m.availableLines()
	end := min(m.viewport+avail, len(m.items))

	// Scroll up indicator
	if m.viewport > 0 {
		b.WriteString(styles.Gray.Render("        ↑ more") + "\n")
	}

	for i := m.viewport; i < end; i++ {
		it := m.items[i]
		isCursor := i == m.cursor

		// Cursor
		cursor := "  "
		if isCursor {
			cursor = styles.BoldCyan.Render("▸ ")
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

		// Version
		versionStr := styles.Gray.Render(it.Version)

		fmt.Fprintf(&b, "  %s  %s  %s\n", cursor, nameStr, versionStr)
	}

	// Scroll down indicator
	if end < len(m.items) {
		b.WriteString(styles.Gray.Render("        ↓ more") + "\n")
	}

	// Summary
	b.WriteString("\n")
	label := fmt.Sprintf("%d unused", len(m.items))
	if len(m.items) == 0 {
		label = "no unused dependencies"
	}
	fmt.Fprintf(&b, "  %s\n", styles.BoldCyan.Render(label))

	// Help bar
	b.WriteString("\n")
	help := []string{
		styles.Bold.Render("↑↓") + " " + styles.Gray.Render("navigate"),
		styles.Bold.Render("q") + " " + styles.Gray.Render("quit"),
	}
	b.WriteString("  " + strings.Join(help, styles.Gray.Render("  ·  ")))
	b.WriteString("\n")

	return b.String()
}

// RunUnused launches the unused dependencies TUI.
func RunUnused(items []UnusedItem, total, scanned int) {
	m := newUnusedModel(items, total, scanned)
	p := tea.NewProgram(m, tea.WithAltScreen())
	p.Run()
}
