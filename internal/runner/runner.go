// Package runner generates and executes npm install commands.
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
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

// exactRe matches a pinned version such as "1.2.3", "=1.2.3" or "v1.2.3-beta.1".
var exactRe = regexp.MustCompile(`^[=v]*\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// installSpec returns the npm install spec for an item, keeping the style of
// the range declared in package.json: "^" and "~" are passed explicitly (so
// npm's save-prefix/save-exact config doesn't rewrite them), and pinned
// versions need --save-exact. Other ranges fall back to npm's default.
func installSpec(it tui.Item) (spec string, exact bool) {
	r := strings.TrimSpace(it.Range)
	switch {
	case strings.HasPrefix(r, "^"):
		return it.Name + "@^" + it.Available, false
	case strings.HasPrefix(r, "~"):
		return it.Name + "@~" + it.Available, false
	case exactRe.MatchString(r):
		return it.Name + "@" + it.Available, true
	default:
		return it.Name + "@" + it.Available, false
	}
}

// GenerateCommands creates npm install commands from selected items, split by
// prod/dev and by whether versions must be saved exactly.
func GenerateCommands(items []tui.Item) []Command {
	type group struct {
		dev, exact bool
	}
	order := []group{{false, false}, {false, true}, {true, false}, {true, true}}
	specs := map[group][]string{}

	for _, it := range items {
		spec, exact := installSpec(it)
		g := group{it.IsDev, exact}
		specs[g] = append(specs[g], spec)
	}

	var cmds []Command
	for _, g := range order {
		if len(specs[g]) == 0 {
			continue
		}
		args := []string{"install"}
		if g.dev {
			args = append(args, "--save-dev")
		}
		if g.exact {
			args = append(args, "--save-exact")
		}
		args = append(args, specs[g]...)
		cmds = append(cmds, Command{
			Args:    args,
			Display: displayCommand(args),
			IsDev:   g.dev,
		})
	}

	return cmds
}

// displayCommand renders args for copy-pasting into a shell, quoting specs
// with characters some shells treat specially (e.g. ^ in zsh extended glob).
func displayCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, "^~") {
			a = "'" + a + "'"
		}
		quoted[i] = a
	}
	return "npm " + strings.Join(quoted, " ")
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
