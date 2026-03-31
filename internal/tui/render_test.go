package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

func testSessions() []session.Session {
	now := time.Now()
	return []session.Session{
		{
			ID:        "2026-03-27-abc123",
			Name:      "auth-refactor",
			Source:    session.SourceSandbox,
			Status:    session.StatusRunning,
			Health:    session.HealthGreen,
			Dir:       "/home/user/dev/myproject",
			Branch:    "sandbox/2026-03-27-abc123",
			StartedAt: now,
			Task:      "Refactor auth middleware",
		},
		{
			ID:        "native-uuid-456",
			Name:      "",
			Source:    session.SourceNative,
			Status:    session.StatusRunning,
			Health:    session.HealthYellow,
			Dir:       "/home/user/dev/other",
			StartedAt: now.Add(-5 * time.Minute),
			Task:      "other",
			Diagnostics: []session.Diagnostic{
				{Signal: "idle", Severity: session.SeverityWarning, Detail: "no activity for 5 minutes"},
			},
		},
		{
			ID:        "2026-03-27-done01",
			Name:      "completed-task",
			Source:    session.SourceSandbox,
			Status:    session.StatusSuccess,
			Health:    session.HealthGreen,
			Dir:       "/home/user/dev/finished",
			StartedAt: now.Add(-2 * time.Hour),
		},
	}
}

func testActivity() []scanner.ActivityEntry {
	base := time.Date(2026, 3, 27, 14, 30, 0, 0, time.UTC)
	return []scanner.ActivityEntry{
		{Time: base, Tool: "Read", Detail: "/workspace/PLAN.md"},
		{Time: base.Add(1 * time.Minute), Tool: "Edit", Detail: "/workspace/internal/auth/middleware.go"},
		{Time: base.Add(2 * time.Minute), Tool: "Bash", Detail: "go test ./internal/auth/...", IsError: true},
		{Time: base.Add(3 * time.Minute), Tool: "Edit", Detail: "/workspace/internal/auth/middleware.go"},
		{Time: base.Add(4 * time.Minute), Tool: "Bash", Detail: "go test ./internal/auth/..."},
	}
}

// --- Session List Rendering ---

func TestSessionList_RendersAllSessions(t *testing.T) {
	sl := newSessionList()
	sl.SetSize(40, 20)
	sl.SetSessions(testSessions())

	output := sl.View()

	t.Run("contains session names", func(t *testing.T) {
		if !strings.Contains(output, "auth-refactor") {
			t.Error("missing session name 'auth-refactor'")
		}
		// Second session has no name, should show ID.
		if !strings.Contains(output, "native-uuid-456") {
			t.Error("missing session ID 'native-uuid-456'")
		}
		if !strings.Contains(output, "completed-task") {
			t.Error("missing session name 'completed-task'")
		}
	})

	t.Run("contains source types", func(t *testing.T) {
		if !strings.Contains(output, "sandbox") {
			t.Error("missing source 'sandbox'")
		}
		if !strings.Contains(output, "native") {
			t.Error("missing source 'native'")
		}
	})

	t.Run("contains statuses", func(t *testing.T) {
		if !strings.Contains(output, "running") {
			t.Error("missing status 'running'")
		}
		if !strings.Contains(output, "success") {
			t.Error("missing status 'success'")
		}
	})

	t.Run("contains health dots", func(t *testing.T) {
		if !strings.Contains(output, "●") {
			t.Error("missing health dot character")
		}
	})
}

func TestSessionList_EmptyRendersPlaceholder(t *testing.T) {
	sl := newSessionList()
	sl.SetSize(40, 10)
	sl.SetSessions(nil)

	output := sl.View()
	if !strings.Contains(output, "No sessions") {
		t.Errorf("expected 'No sessions' placeholder, got:\n%s", output)
	}
}

func TestSessionList_SelectedSessionHighlighted(t *testing.T) {
	sl := newSessionList()
	sl.SetSize(40, 20)
	sl.SetSessions(testSessions())

	// First session is selected by default (cursor = 0).
	output := sl.View()
	if !strings.Contains(output, "auth-refactor") {
		t.Error("selected session 'auth-refactor' not in output")
	}

	// Move cursor down and verify second session is still visible.
	sl.Down()
	output = sl.View()
	if !strings.Contains(output, "native-uuid-456") {
		t.Error("second session not in output after cursor move")
	}
}

// --- Detail Pane Rendering ---

func TestDetailPane_RendersSessionInfo(t *testing.T) {
	d := newDetailPane()
	d.SetSize(60, 30)

	sess := testSessions()[0]
	activity := testActivity()
	d.SetSession(&sess, activity)

	output := d.View()

	t.Run("contains source", func(t *testing.T) {
		if !strings.Contains(output, "sandbox") {
			t.Error("missing source")
		}
	})

	t.Run("contains directory", func(t *testing.T) {
		if !strings.Contains(output, "/home/user/dev/myproject") {
			t.Error("missing directory")
		}
	})

	t.Run("contains branch", func(t *testing.T) {
		if !strings.Contains(output, "sandbox/2026-03-27-abc123") {
			t.Error("missing branch")
		}
	})

	t.Run("contains task", func(t *testing.T) {
		if !strings.Contains(output, "Refactor auth middleware") {
			t.Error("missing task")
		}
	})

	t.Run("contains activity section", func(t *testing.T) {
		if !strings.Contains(output, "Recent Activity") {
			t.Error("missing 'Recent Activity' header")
		}
	})

	t.Run("contains tool names from activity", func(t *testing.T) {
		if !strings.Contains(output, "Read") {
			t.Error("missing Read tool in activity")
		}
		if !strings.Contains(output, "Edit") {
			t.Error("missing Edit tool in activity")
		}
		if !strings.Contains(output, "Bash") {
			t.Error("missing Bash tool in activity")
		}
	})

	t.Run("contains activity timestamps", func(t *testing.T) {
		if !strings.Contains(output, "14:30") {
			t.Error("missing timestamp 14:30")
		}
	})

	t.Run("contains file paths from activity", func(t *testing.T) {
		// detail.go uses filepath.Base on the Detail field.
		if !strings.Contains(output, "PLAN.md") {
			t.Error("missing PLAN.md in activity detail")
		}
		if !strings.Contains(output, "middleware.go") {
			t.Error("missing middleware.go in activity detail")
		}
	})
}

func TestDetailPane_RendersDiagnostics(t *testing.T) {
	d := newDetailPane()
	d.SetSize(60, 30)

	sess := testSessions()[1] // The one with diagnostics.
	d.SetSession(&sess, nil)

	output := d.View()

	if !strings.Contains(output, "no activity for 5 minutes") {
		t.Errorf("missing diagnostic detail in output:\n%s", output)
	}
	if !strings.Contains(output, "\u26a0") {
		t.Error("missing warning icon")
	}
}

func TestDetailPane_CriticalDiagnosticUsesErrorIcon(t *testing.T) {
	d := newDetailPane()
	d.SetSize(60, 30)

	sess := session.Session{
		ID:     "test",
		Source: session.SourceSandbox,
		Status: session.StatusRunning,
		Dir:    "/test",
		Diagnostics: []session.Diagnostic{
			{Signal: "test-loop", Severity: session.SeverityCritical, Detail: "tests failing 3 times"},
		},
	}
	d.SetSession(&sess, nil)

	output := d.View()

	if !strings.Contains(output, "\u2716") {
		t.Error("missing critical icon")
	}
	if !strings.Contains(output, "tests failing 3 times") {
		t.Error("missing critical diagnostic detail")
	}
}

// --- Status Bar Rendering ---

func TestStatusBar_RendersKeyBindings(t *testing.T) {
	sb := newStatusBar()
	sb.SetWidth(100)

	output := sb.View()

	bindings := []string{"navigate", "peek", "new", "stop", "clean", "label", "help", "quit"}
	for _, b := range bindings {
		if !strings.Contains(output, b) {
			t.Errorf("missing keybinding hint %q in status bar", b)
		}
	}
}

func TestStatusBar_HelpViewShowsAllBindings(t *testing.T) {
	sb := newStatusBar()
	sb.SetWidth(100)
	sb.ToggleHelp()

	output := sb.View()

	commands := []string{
		"Navigate sessions",
		"Toggle peek",
		"New session",
		"Stop selected session",
		"Clean completed/failed",
		"Label selected session",
		"Toggle this help",
		"Quit",
	}
	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("missing help entry %q", cmd)
		}
	}
}

// --- Full Layout Rendering ---

func TestApp_FullRenderContainsSessions(t *testing.T) {
	app := NewApp(nil)
	app.width = 120
	app.height = 40

	sessions := testSessions()
	activity := testActivity()

	app.sessions.SetSessions(sessions)
	app.activities = map[string][]scanner.ActivityEntry{
		sessions[0].ID: activity,
	}
	app.updateDetail()

	// Verify View() does not panic with real data at full layout size.
	view := app.View()
	_ = view

	// Verify the individual component views contain expected content.
	listWidth := app.width * 30 / 100
	detailWidth := app.width - listWidth
	contentHeight := app.height - 2

	app.sessions.SetSize(listWidth-2, contentHeight-2)
	app.detail.SetSize(detailWidth-2, contentHeight-2)

	listView := app.sessions.View()
	detailView := app.detail.View()

	t.Run("list contains all session names", func(t *testing.T) {
		if !strings.Contains(listView, "auth-refactor") {
			t.Error("missing auth-refactor in list")
		}
		if !strings.Contains(listView, "completed-task") {
			t.Error("missing completed-task in list")
		}
	})

	t.Run("detail shows selected session info", func(t *testing.T) {
		if !strings.Contains(detailView, "Refactor auth middleware") {
			t.Error("missing task in detail")
		}
		if !strings.Contains(detailView, "Recent Activity") {
			t.Error("missing activity section in detail")
		}
	})

	t.Run("detail shows activity from log", func(t *testing.T) {
		if !strings.Contains(detailView, "middleware.go") {
			t.Error("missing activity file reference")
		}
	})
}

func TestApp_RenderWithNoSessions(t *testing.T) {
	app := NewApp(nil)
	app.width = 80
	app.height = 24

	// No sessions injected; test empty state does not panic.
	view := app.View()
	_ = view

	app.sessions.SetSize(22, 20)
	output := app.sessions.View()
	if !strings.Contains(output, "No sessions") {
		t.Error("expected 'No sessions' in empty list render")
	}
}

func TestApp_RenderZeroSizeDoesNotPanic(t *testing.T) {
	app := NewApp(nil)
	// width/height are 0 by default; should return loading view.
	view := app.View()
	if view.Content != "Loading..." {
		t.Errorf("expected 'Loading...' content, got %q", view.Content)
	}
}
