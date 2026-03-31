package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

type detailPane struct {
	session          *session.Session
	activity         []scanner.ActivityEntry
	lastMessage      string
	conversationTail []string
	width            int
	height           int
	peeking          bool
}

func newDetailPane() detailPane {
	return detailPane{}
}

func (d *detailPane) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *detailPane) SetSession(s *session.Session, activity []scanner.ActivityEntry, lastMessage string, conversationTail []string) {
	d.session = s
	d.activity = activity
	d.lastMessage = lastMessage
	d.conversationTail = conversationTail
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

	sections = append(sections, d.renderInfo(s))
	sections = append(sections, "")

	if d.peeking {
		// Peek mode: show conversation tail (Claude's recent messages).
		sections = append(sections, d.renderConversation()...)
	} else {
		// Normal mode: last message + activity + diagnostics.
		if d.lastMessage != "" {
			msgDivider := detailSectionStyle.Render("── Last Message " + strings.Repeat("─", max(0, d.width-19)))
			sections = append(sections, msgDivider, "")
			msg := truncateMessage(d.lastMessage, d.width-4, 3)
			sections = append(sections, "  "+detailValueStyle.Render(msg))
			sections = append(sections, "")
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
			toolName := shortenToolName(a.Tool)
			tool := activityToolStyle.Render(fmt.Sprintf("%-8s", toolName))
			detail := filepath.Base(a.Detail)
			detailRendered := activityDetailStyle.Render(detail)
			line := fmt.Sprintf("  %s  %s %s", activityTimeStyle.Render(timeStr), tool, detailRendered)
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
	}

	content := strings.Join(sections, "\n")
	return padToHeight(content, d.height)
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

// shortenToolName truncates long MCP tool names to a readable form.
func shortenToolName(name string) string {
	// MCP tools look like "mcp__notion__API-get-block-children" — show just the action part.
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(name, "__", 3)
		if len(parts) == 3 {
			// "mcp__notion__API-get-block-children" -> "notion"
			return parts[1]
		}
	}
	if len(name) > 8 {
		return name[:8]
	}
	return name
}

func (d *detailPane) renderConversation() []string {
	divider := detailSectionStyle.Render("── Conversation " + strings.Repeat("─", max(0, d.width-19)))
	sections := []string{divider, ""}

	if len(d.conversationTail) == 0 {
		sections = append(sections, "  "+lipgloss.NewStyle().Foreground(colorGray).Render("No conversation data"))
		return sections
	}

	// Use most of the pane for conversation. Each message gets up to 2 lines.
	maxMessages := (d.height - 8) / 3 // 2 lines per message + 1 blank line.
	if maxMessages < 1 {
		maxMessages = 1
	}

	start := 0
	if len(d.conversationTail) > maxMessages {
		start = len(d.conversationTail) - maxMessages
	}

	for i := start; i < len(d.conversationTail); i++ {
		msg := truncateMessage(d.conversationTail[i], d.width-4, 2)
		sections = append(sections, "  "+detailValueStyle.Render(msg))
		sections = append(sections, "")
	}

	return sections
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
