package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

func keyPress(code rune) tea.KeyPressMsg {
	msg := tea.KeyPressMsg{Code: code}
	// Printable characters need Text set for String() to return the char.
	// Exclude control/special keys.
	if code >= 32 && code < 128 {
		msg.Text = string(code)
	}
	return msg
}

func TestApp_KeyNavigation(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "first", Source: session.SourceSandbox, Health: session.HealthGreen},
		{ID: "s2", Name: "second", Source: session.SourceNative, Health: session.HealthYellow},
	}

	t.Run("initial cursor at 0", func(t *testing.T) {
		if app.sessions.cursor != 0 {
			t.Errorf("cursor = %d, want 0", app.sessions.cursor)
		}
	})

	t.Run("down moves cursor", func(t *testing.T) {
		updated, _ := app.Update(keyPress('j'))
		app = updated.(*App)
		if app.sessions.cursor != 1 {
			t.Errorf("cursor = %d, want 1", app.sessions.cursor)
		}
	})

	t.Run("down at bottom stays", func(t *testing.T) {
		updated, _ := app.Update(keyPress('j'))
		app = updated.(*App)
		if app.sessions.cursor != 1 {
			t.Errorf("cursor = %d, want 1", app.sessions.cursor)
		}
	})

	t.Run("up moves cursor back", func(t *testing.T) {
		updated, _ := app.Update(keyPress('k'))
		app = updated.(*App)
		if app.sessions.cursor != 0 {
			t.Errorf("cursor = %d, want 0", app.sessions.cursor)
		}
	})

	t.Run("up at top stays", func(t *testing.T) {
		updated, _ := app.Update(keyPress('k'))
		app = updated.(*App)
		if app.sessions.cursor != 0 {
			t.Errorf("cursor = %d, want 0", app.sessions.cursor)
		}
	})

	t.Run("question mark toggles help", func(t *testing.T) {
		updated, _ := app.Update(keyPress('?'))
		app = updated.(*App)
		if !app.statusbar.showHelp {
			t.Error("showHelp = false, want true")
		}

		updated, _ = app.Update(keyPress('?'))
		app = updated.(*App)
		if app.statusbar.showHelp {
			t.Error("showHelp = true, want false")
		}
	})

	t.Run("enter toggles peek", func(t *testing.T) {
		updated, _ := app.Update(keyPress(tea.KeyEnter))
		app = updated.(*App)
		if !app.detail.peeking {
			t.Error("peeking = false, want true")
		}
	})

	t.Run("l enters label mode", func(t *testing.T) {
		updated, _ := app.Update(keyPress('l'))
		app = updated.(*App)
		if app.mode != modeLabel {
			t.Errorf("mode = %d, want modeLabel (%d)", app.mode, modeLabel)
		}
	})

	t.Run("escape exits label mode", func(t *testing.T) {
		updated, _ := app.Update(keyPress(tea.KeyEscape))
		app = updated.(*App)
		if app.mode != modeNormal {
			t.Errorf("mode = %d, want modeNormal", app.mode)
		}
	})

	t.Run("s enters confirm mode", func(t *testing.T) {
		updated, _ := app.Update(keyPress('s'))
		app = updated.(*App)
		if app.mode != modeConfirm {
			t.Errorf("mode = %d, want modeConfirm (%d)", app.mode, modeConfirm)
		}
		if app.confirmAction != confirmStop {
			t.Errorf("confirmAction = %d, want confirmStop", app.confirmAction)
		}
	})

	t.Run("n in confirm cancels", func(t *testing.T) {
		updated, _ := app.Update(keyPress('n'))
		app = updated.(*App)
		if app.mode != modeNormal {
			t.Errorf("mode = %d, want modeNormal after cancel", app.mode)
		}
	})
}

func TestApp_TickUpdatesSessions(t *testing.T) {
	app := NewApp(nil)

	sessions := []session.Session{
		{ID: "s1", Name: "test", LastActivity: time.Now()},
	}
	msg := tickMsg{sessions: sessions}
	updated, _ := app.Update(msg)
	app = updated.(*App)

	if len(app.sessions.sessions) != 1 {
		t.Errorf("sessions count = %d, want 1", len(app.sessions.sessions))
	}
	if app.sessions.sessions[0].ID != "s1" {
		t.Errorf("session ID = %s, want s1", app.sessions.sessions[0].ID)
	}
}

func TestApp_TickErrorPreservesState(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "existing", Name: "keep"},
	}

	msg := tickMsg{err: fmt.Errorf("scan failed")}
	updated, _ := app.Update(msg)
	app = updated.(*App)

	if len(app.sessions.sessions) != 1 {
		t.Errorf("sessions count = %d, want 1 (preserved)", len(app.sessions.sessions))
	}
	if app.sessions.sessions[0].ID != "existing" {
		t.Errorf("session ID = %s, want existing", app.sessions.sessions[0].ID)
	}
}

func TestSessionList_CursorClamp(t *testing.T) {
	sl := newSessionList()
	sl.sessions = []session.Session{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	sl.cursor = 2

	sl.SetSessions([]session.Session{{ID: "x"}})
	if sl.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after shrink", sl.cursor)
	}
}

func TestSessionList_EmptySelected(t *testing.T) {
	sl := newSessionList()
	if sl.Selected() != nil {
		t.Error("Selected() should be nil for empty list")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"seconds", time.Now().Add(-30 * time.Second), "30s"},
		{"minutes", time.Now().Add(-5 * time.Minute), "5m"},
		{"hours", time.Now().Add(-3 * time.Hour), "3h"},
		{"days", time.Now().Add(-48 * time.Hour), "2d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.when)
			if got != tt.want {
				t.Errorf("formatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHealthDot(t *testing.T) {
	// Verify each health produces non-empty output.
	for _, h := range []session.Health{session.HealthGreen, session.HealthYellow, session.HealthRed, ""} {
		got := healthDot(h)
		if got == "" {
			t.Errorf("healthDot(%q) returned empty", h)
		}
	}
}

func TestDetailPane_NilSession(t *testing.T) {
	d := newDetailPane()
	got := d.View()
	if got != "  Select a session" {
		t.Errorf("View() = %q, want 'Select a session'", got)
	}
}

func TestStatusBar_HelpToggle(t *testing.T) {
	sb := newStatusBar()
	sb.SetWidth(80)

	normal := sb.View()
	sb.ToggleHelp()
	help := sb.View()

	if normal == help {
		t.Error("help view should differ from normal view")
	}
}
