package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

type sessionList struct {
	sessions []session.Session
	cursor   int
	width    int
	height   int
}

func newSessionList() sessionList {
	return sessionList{}
}

func (sl *sessionList) SetSize(w, h int) {
	sl.width = w
	sl.height = h
}

func (sl *sessionList) SetSessions(sessions []session.Session) {
	// Preserve cursor on the same session across refreshes.
	var selectedID string
	if sl.cursor < len(sl.sessions) {
		selectedID = sl.sessions[sl.cursor].ID
	}

	sl.sessions = sessions

	// Try to restore cursor to the same session.
	if selectedID != "" {
		for i, s := range sessions {
			if s.ID == selectedID {
				sl.cursor = i
				return
			}
		}
	}
	// Fallback: clamp cursor.
	if sl.cursor >= len(sessions) && len(sessions) > 0 {
		sl.cursor = len(sessions) - 1
	}
}

func (sl *sessionList) Up() {
	if sl.cursor > 0 {
		sl.cursor--
	}
}

func (sl *sessionList) Down() {
	if sl.cursor < len(sl.sessions)-1 {
		sl.cursor++
	}
}

func (sl *sessionList) Selected() *session.Session {
	if sl.cursor < len(sl.sessions) {
		return &sl.sessions[sl.cursor]
	}
	return nil
}

func (sl *sessionList) View() string {
	if len(sl.sessions) == 0 {
		return lipgloss.Place(sl.width, sl.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(colorGray).Render("No sessions"))
	}

	var lines []string
	for i, s := range sl.sessions {
		lines = append(lines, sl.renderSession(i, s)...)
	}

	content := strings.Join(lines, "\n")
	rendered := strings.Count(content, "\n") + 1
	for rendered < sl.height {
		content += "\n"
		rendered++
	}
	return content
}

func (sl *sessionList) renderSession(idx int, s session.Session) []string {
	selected := idx == sl.cursor

	dot := healthDot(s.Health)
	name := s.Name
	if name == "" {
		name = s.ID
	}
	if selected {
		name = sessionNameSelectedStyle.Render(name)
	} else {
		name = sessionNameStyle.Render(name)
	}
	age := formatAge(s.StartedAt)

	line1 := fmt.Sprintf("  %s %s %s", dot, name, lipgloss.NewStyle().Foreground(colorDim).Render(age))
	displayStatus := string(s.Status)
	if s.Status == session.StatusRunning && hasIdleDiagnostic(s) {
		displayStatus = "idle"
	}
	meta := fmt.Sprintf("%s · %s", s.Source, displayStatus)
	if s.Task != "" {
		meta += " · " + s.Task
	}
	line2 := sessionMetaStyle.Render(meta)

	return []string{line1, line2, ""}
}

func hasIdleDiagnostic(s session.Session) bool {
	for _, d := range s.Diagnostics {
		if d.Signal == "idle" {
			return true
		}
	}
	return false
}

func healthDot(h session.Health) string {
	switch h {
	case session.HealthGreen:
		return healthGreenStyle.Render("●")
	case session.HealthYellow:
		return healthYellowStyle.Render("●")
	case session.HealthRed:
		return healthRedStyle.Render("●")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("○")
	}
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
