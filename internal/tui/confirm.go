package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jee4nc/packwatch/internal/styles"
)

type confirmModel struct {
	prompt   string
	accepted bool
	done     bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "y":
			m.accepted = true
			m.done = true
			return m, tea.Quit
		case "n", "q", "esc", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.done {
		answer := styles.Red.Render("No")
		if m.accepted {
			answer = styles.Green.Render("Yes")
		}
		return fmt.Sprintf("\n  %s %s %s\n",
			styles.BoldCyan.Render("?"), m.prompt, answer)
	}
	return fmt.Sprintf("\n  %s %s %s ",
		styles.BoldCyan.Render("?"), m.prompt, styles.Gray.Render("(y/n)"))
}

// Confirm displays a y/n prompt and returns the answer.
func Confirm(prompt string) bool {
	m := confirmModel{prompt: prompt}
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return false
	}
	return result.(confirmModel).accepted
}
