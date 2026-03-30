// Package runner generates and executes npm install commands.
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jee4nc/packwatch/internal/styles"
	"github.com/jee4nc/packwatch/internal/tui"
)

// Command holds a generated npm install command.
type Command struct {
	Args    []string
	Display string
	IsDev   bool
}

// GenerateCommands creates npm install commands from selected items.
func GenerateCommands(items []tui.Item) []Command {
	var prodPkgs, devPkgs []string

	for _, it := range items {
		spec := it.Name + "@" + it.Available
		if it.IsDev {
			devPkgs = append(devPkgs, spec)
		} else {
			prodPkgs = append(prodPkgs, spec)
		}
	}

	var cmds []Command

	if len(prodPkgs) > 0 {
		args := append([]string{"install"}, prodPkgs...)
		cmds = append(cmds, Command{
			Args:    args,
			Display: "npm " + strings.Join(args, " "),
		})
	}

	if len(devPkgs) > 0 {
		args := append([]string{"install", "--save-dev"}, devPkgs...)
		cmds = append(cmds, Command{
			Args:    args,
			Display: "npm " + strings.Join(args, " "),
			IsDev:   true,
		})
	}

	return cmds
}

// PrintCommands displays the commands that would be run.
func PrintCommands(cmds []Command) {
	fmt.Println()
	fmt.Println("  " + styles.BoldCyan.Render(styles.Emoji("🚀 ")+"Commands to run:"))
	fmt.Println()
	for _, cmd := range cmds {
		label := styles.Green.Render("prod")
		if cmd.IsDev {
			label = styles.Yellow.Render("dev ")
		}
		fmt.Printf("  %s  %s\n", label, styles.Bold.Render(cmd.Display))
	}
}

// GenerateUninstallCommands creates npm uninstall commands from selected unused items.
func GenerateUninstallCommands(items []tui.UnusedItem) []Command {
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}

	if len(names) == 0 {
		return nil
	}

	args := append([]string{"uninstall"}, names...)
	return []Command{{
		Args:    args,
		Display: "npm " + strings.Join(args, " "),
	}}
}

// Execute runs the npm commands with real-time output.
func Execute(cmds []Command) error {
	for _, cmd := range cmds {
		fmt.Printf("\n  %s %s\n\n", styles.Emoji("▶ "), styles.Bold.Render(cmd.Display))

		c := exec.Command("npm", cmd.Args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin

		if err := c.Run(); err != nil {
			return fmt.Errorf("command failed: %s: %w", cmd.Display, err)
		}

		fmt.Printf("  %s %s\n", styles.Emoji("✅ "), styles.Green.Render("Done"))
	}
	return nil
}
