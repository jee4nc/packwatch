package styles

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var noColor bool

// Palette — catppuccin Mocha inspired.
var (
	colorCyan     = lipgloss.Color("#89B4FA")
	colorGreen    = lipgloss.Color("#A6E3A1")
	colorYellow   = lipgloss.Color("#F9E2AF")
	colorRed      = lipgloss.Color("#F38BA8")
	colorMagenta  = lipgloss.Color("#CBA6F7")
	colorGray     = lipgloss.Color("#7F849C")
	colorDimGreen = lipgloss.Color("#6C9C78")
)

// Styles — initialized with colors; reset to plain by Init(true).
var (
	Bold  = lipgloss.NewStyle().Bold(true)
	Faint = lipgloss.NewStyle().Faint(true)

	Cyan    = lipgloss.NewStyle().Foreground(colorCyan)
	Green   = lipgloss.NewStyle().Foreground(colorGreen)
	Yellow  = lipgloss.NewStyle().Foreground(colorYellow)
	Red     = lipgloss.NewStyle().Foreground(colorRed)
	Magenta = lipgloss.NewStyle().Foreground(colorMagenta)
	Gray    = lipgloss.NewStyle().Foreground(colorGray)

	DimGreen = lipgloss.NewStyle().Foreground(colorDimGreen)

	BoldCyan   = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	BoldGreen  = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	BoldYellow = lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	BoldRed     = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	BoldMagenta = lipgloss.NewStyle().Bold(true).Foreground(colorMagenta)

	Banner = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Padding(0, 2).
		Foreground(colorCyan).
		Bold(true)
)

// Init sets up color mode. Call before any rendering.
func Init(forceNoColor bool) {
	noColor = forceNoColor || os.Getenv("NO_COLOR") != ""
	if noColor {
		os.Setenv("NO_COLOR", "1")
		p := lipgloss.NewStyle()
		Bold = p
		Faint = p
		Cyan = p
		Green = p
		Yellow = p
		Red = p
		Magenta = p
		Gray = p
		DimGreen = p
		BoldCyan = p
		BoldGreen = p
		BoldYellow = p
		BoldRed = p
		BoldMagenta = p
		Banner = p
	}
}

// Emoji returns the emoji when colors are enabled, empty string otherwise.
func Emoji(e string) string {
	if noColor {
		return ""
	}
	return e
}

// UpdateTypeStyle returns the style for a given update type tag.
func UpdateTypeStyle(updateType string) lipgloss.Style {
	switch strings.ToLower(updateType) {
	case "major":
		return BoldRed
	case "minor":
		return BoldYellow
	case "patch":
		return BoldGreen
	default:
		return Gray
	}
}

// UpdateTypeBadge renders a styled badge for the update type.
func UpdateTypeBadge(updateType string) string {
	label := strings.ToUpper(updateType)
	style := UpdateTypeStyle(updateType)
	return style.Render("‹" + label + "›")
}

// UpdateTypeArrow returns a styled arrow colored by update type.
func UpdateTypeArrow(updateType string) string {
	return UpdateTypeStyle(updateType).Render("→")
}

// ProgressBar renders a modern ━/─ progress bar.
func ProgressBar(current, total, width int) string {
	if total == 0 {
		return ""
	}
	pct := float64(current) / float64(total)
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
	pctStr := fmt.Sprintf("%3.0f%%", pct*100)
	return fmt.Sprintf("  %s %s  %d/%d",
		Cyan.Render(bar), Gray.Render(pctStr), current, total)
}

// SeverityStyle returns the appropriate style for a severity level.
func SeverityStyle(severity string) lipgloss.Style {
	switch severity {
	case "CRITICAL":
		return BoldMagenta
	case "HIGH":
		return BoldRed
	case "MEDIUM":
		return BoldYellow
	case "LOW":
		return Green
	default:
		return Gray
	}
}

// VulnBadge renders a compact severity indicator.
func VulnBadge(count int, severity string) string {
	return SeverityStyle(severity).Render("⚠ " + severity)
}

// Separator renders a section divider: ── label ────────
func Separator(label string, width int) string {
	if width <= 0 {
		width = 60
	}
	if label == "" {
		return Gray.Render(strings.Repeat("─", width))
	}
	prefix := "── "
	remaining := width - len(prefix) - len(label) - 3
	if remaining < 4 {
		remaining = 4
	}
	return Gray.Render(prefix) + BoldCyan.Render(label) + " " + Gray.Render(strings.Repeat("─", remaining))
}
