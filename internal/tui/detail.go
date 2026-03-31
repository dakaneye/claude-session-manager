package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

type detailPane struct {
	session     *session.Session
	activity    []scanner.ActivityEntry
	lastMessage string
	width       int
	height      int
	peeking     bool
}

func newDetailPane() detailPane {
	return detailPane{}
}

func (d *detailPane) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *detailPane) SetSession(s *session.Session, activity []scanner.ActivityEntry, lastMessage string) {
	d.session = s
	d.activity = activity
	d.lastMessage = lastMessage
}

func (d *detailPane) TogglePeek() {
	d.peeking = !d.peeking
}

func (d *detailPane) View() string {
	if d.session == nil {
		return "  Select a session"
	}

	s := d.session
	var sections []string

	if !d.peeking {
		sections = append(sections, d.renderInfo(s))
		sections = append(sections, "")

		if d.lastMessage != "" {
			msgDivider := detailSectionStyle.Render("── Last Message " + strings.Repeat("─", max(0, d.width-19)))
			sections = append(sections, msgDivider, "")
			msg := truncateMessage(d.lastMessage, d.width-4, 3)
			sections = append(sections, "  "+detailValueStyle.Render(msg))
			sections = append(sections, "")
		}
	}

	divider := detailSectionStyle.Render("── Recent Activity " + strings.Repeat("─", max(0, d.width-22)))
	sections = append(sections, divider, "")

	maxEntries := d.height - len(sections) - 4
	if maxEntries < 1 {
		maxEntries = 1
	}
	start := 0
	if len(d.activity) > maxEntries {
		start = len(d.activity) - maxEntries
	}
	for _, a := range d.activity[start:] {
		timeStr := ""
		if !a.Time.IsZero() {
			timeStr = a.Time.Format("15:04")
		}
		tool := activityToolStyle.Render(a.Tool)

		// In peek mode, show full paths and error markers; otherwise show basename.
		detail := a.Detail
		if !d.peeking {
			detail = filepath.Base(detail)
		}
		if d.peeking && a.IsError {
			detail = "✖ " + detail
		}
		detailRendered := activityDetailStyle.Render(detail)
		line := fmt.Sprintf("  %s  %s  %s", activityTimeStyle.Render(timeStr), tool, detailRendered)
		sections = append(sections, line)
	}

	if len(s.Diagnostics) > 0 {
		sections = append(sections, "")
		for _, diag := range s.Diagnostics {
			icon := "⚠"
			style := diagnosticWarningStyle
			if diag.Severity == session.SeverityCritical {
				icon = "✖"
				style = diagnosticCriticalStyle
			}
			sections = append(sections, "  "+style.Render(icon+" "+diag.Detail))
		}
	}

	content := strings.Join(sections, "\n")
	rendered := strings.Count(content, "\n") + 1
	for rendered < d.height {
		content += "\n"
		rendered++
	}
	return content
}

// truncateMessage limits a message to maxLines lines, each at most maxWidth runes.
func truncateMessage(msg string, maxWidth, maxLines int) string {
	lines := strings.SplitN(msg, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] += "..."
	}
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) > maxWidth {
			lines[i] = string(runes[:maxWidth-3]) + "..."
		}
	}
	return strings.Join(lines, "\n  ")
}

func (d *detailPane) renderInfo(s *session.Session) string {
	var lines []string
	line := func(label, value string) string {
		return fmt.Sprintf("  %s %s", detailLabelStyle.Render(label), detailValueStyle.Render(value))
	}

	lines = append(lines, line("Source:", string(s.Source)))
	lines = append(lines, line("Dir:   ", s.Dir))
	if s.Branch != "" {
		lines = append(lines, line("Branch:", s.Branch))
	}
	if s.Task != "" {
		lines = append(lines, line("Task:  ", s.Task))
	}
	return strings.Join(lines, "\n")
}
