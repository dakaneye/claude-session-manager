package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type statusBar struct {
	width    int
	showHelp bool
}

func newStatusBar() statusBar {
	return statusBar{}
}

func (sb *statusBar) SetWidth(w int) {
	sb.width = w
}

func (sb *statusBar) ToggleHelp() {
	sb.showHelp = !sb.showHelp
}

func (sb *statusBar) View() string {
	if sb.showHelp {
		return sb.helpView()
	}

	bindings := []struct{ key, desc string }{
		{"↑↓", "navigate"},
		{"enter", "peek"},
		{"n", "new"},
		{"a", "attach"},
		{"s", "stop"},
		{"c", "clean"},
		{"l", "label"},
		{"?", "help"},
		{"q", "quit"},
	}

	var parts []string
	for _, b := range bindings {
		parts = append(parts, statusBarKeyStyle.Render(b.key)+" "+statusBarStyle.Render(b.desc))
	}

	line := " " + strings.Join(parts, statusBarStyle.Render(" · "))
	return lipgloss.NewStyle().Width(sb.width).Render(line)
}

func (sb *statusBar) helpView() string {
	help := []string{
		"  " + statusBarKeyStyle.Render("↑/↓ or j/k") + "  Navigate sessions",
		"  " + statusBarKeyStyle.Render("enter") + "      Toggle peek (scrollable log)",
		"  " + statusBarKeyStyle.Render("n") + "          New session",
		"  " + statusBarKeyStyle.Render("a") + "          Attach to native session",
		"  " + statusBarKeyStyle.Render("s") + "          Stop selected session",
		"  " + statusBarKeyStyle.Render("c") + "          Clean completed/failed",
		"  " + statusBarKeyStyle.Render("l") + "          Label selected session",
		"  " + statusBarKeyStyle.Render("?") + "          Toggle this help",
		"  " + statusBarKeyStyle.Render("q") + "          Quit (sessions keep running)",
	}
	return strings.Join(help, "\n")
}

// RenderInput renders the status bar in text input mode.
func (sb *statusBar) RenderInput(prompt, text string) string {
	content := " " + statusBarKeyStyle.Render(prompt) + statusBarStyle.Render(text+"_")
	return lipgloss.NewStyle().Width(sb.width).Render(content)
}

// RenderConfirm renders the status bar in confirmation mode.
func (sb *statusBar) RenderConfirm(prompt string) string {
	content := " " + statusBarKeyStyle.Render(prompt)
	return lipgloss.NewStyle().Width(sb.width).Render(content)
}

// RenderFlash renders a temporary flash message.
func (sb *statusBar) RenderFlash(msg string) string {
	content := " " + lipgloss.NewStyle().Foreground(colorAccent).Render(msg)
	return lipgloss.NewStyle().Width(sb.width).Render(content)
}
