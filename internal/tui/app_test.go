package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ptyPkg "github.com/dakaneye/claude-session-manager/internal/pty"
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
	app := NewApp(nil, nil)
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
	app := NewApp(nil, nil)

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
	app := NewApp(nil, nil)
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

	if sb.showHelp {
		t.Error("showHelp should start false")
	}
	sb.ToggleHelp()
	if !sb.showHelp {
		t.Error("ToggleHelp should set showHelp to true")
	}
	sb.ToggleHelp()
	if sb.showHelp {
		t.Error("ToggleHelp should set showHelp back to false")
	}
}

func TestApp_AttachDiscoveredSession(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "d1", Name: "discovered", Source: session.SourceNative, Health: session.HealthGreen, Managed: false, Status: session.StatusRunning},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	if !strings.Contains(app.flashMsg, "not managed") {
		t.Errorf("flash = %q, want to contain 'not managed'", app.flashMsg)
	}
}

func TestApp_AttachStoppedManagedSession(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "stopped", Source: session.SourceNative, Health: session.HealthGreen, Managed: true, Status: session.StatusStopped},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	if app.mode != modeConfirm {
		t.Errorf("mode = %d, want modeConfirm", app.mode)
	}
	if app.confirmAction != confirmResume {
		t.Errorf("confirmAction = %d, want confirmResume", app.confirmAction)
	}
}

func TestApp_AttachSandboxBetweenStages(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{
			ID:      "sb1",
			Name:    "sandbox-ready",
			Source:  session.SourceSandbox,
			Health:  session.HealthGreen,
			Managed: true,
			Status:  session.StatusReady,
		},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	if app.mode != modeConfirm {
		t.Errorf("mode = %d, want modeConfirm", app.mode)
	}
	if app.confirmAction != confirmNextStage {
		t.Errorf("confirmAction = %d, want confirmNextStage", app.confirmAction)
	}
}

func TestSessionList_ManagedIndicator(t *testing.T) {
	sl := newSessionList()
	sl.width = 60
	sl.height = 20
	sl.sessions = []session.Session{
		{ID: "m1", Name: "managed-one", Source: session.SourceNative, Health: session.HealthGreen, Managed: true},
		{ID: "d1", Name: "discovered", Source: session.SourceNative, Health: session.HealthGreen, Managed: false},
	}

	view := sl.View()

	if !strings.Contains(view, "[cs]") {
		t.Error("managed session should show [cs] indicator")
	}
}

func TestApp_ConfirmStopPrompt(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "my-session", Source: session.SourceNative, Health: session.HealthGreen, Status: session.StatusRunning, PID: 12345},
	}

	// Enter confirm stop mode.
	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)

	prompt := app.confirmPrompt()
	if !strings.Contains(prompt, "Stop my-session") {
		t.Errorf("prompt = %q, want to contain 'Stop my-session'", prompt)
	}

	// Cancel with 'n'.
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after cancel", app.mode)
	}
}

func TestApp_ConfirmStopExecutes(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "stoppable", Source: session.SourceNative, Health: session.HealthGreen, Status: session.StatusRunning, PID: 0},
	}

	// Enter confirm stop.
	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)

	// Confirm with 'y' — PID is 0, so stop will flash "not supported".
	updated, _ = app.Update(keyPress('y'))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after confirm", app.mode)
	}
	if !strings.Contains(app.flashMsg, "not supported") {
		t.Errorf("flash = %q, want to contain 'not supported'", app.flashMsg)
	}
}

func TestApp_StopRunningManagedKeepsMetadata(t *testing.T) {
	// Orphaned running managed session (no ptyMgr ownership).
	// Stop should SIGTERM but leave metadata (it would show as
	// stopped on next scan, allowing resume).
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "running-1", Name: "running", Source: session.SourceNative, Managed: true, Status: session.StatusRunning, PID: 0},
	}

	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('y'))
	app = updated.(*App)

	if strings.Contains(app.flashMsg, "Removed") {
		t.Errorf("flash = %q, running session should not be 'Removed'", app.flashMsg)
	}
}

func TestApp_StopStoppedManagedRemoves(t *testing.T) {
	// Create a stopped managed session with a fake metadata file,
	// then verify executeStop removes the metadata.
	tmpDir := t.TempDir()
	metaPath := tmpDir + "/stopped-1.json"
	if err := os.WriteFile(metaPath, []byte(`{"id":"stopped-1"}`), 0o644); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	mgr := ptyPkg.NewManager(tmpDir)
	app := NewApp(nil, mgr)
	app.sessions.sessions = []session.Session{
		{ID: "stopped-1", Name: "stopped", Source: session.SourceNative, Managed: true, Status: session.StatusStopped},
	}

	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('y'))
	app = updated.(*App)

	if !strings.Contains(app.flashMsg, "Removed") {
		t.Errorf("flash = %q, want to contain 'Removed'", app.flashMsg)
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Errorf("metadata file should be removed, got: %v", err)
	}
}

func TestApp_StopConfirmPromptSandboxCleanup(t *testing.T) {
	// A non-managed sandbox session in a terminal state (failed, ready,
	// etc.) should prompt with "Clean X?" since we'll delegate to
	// `claude-sandbox clean`.
	cases := []session.Status{
		session.StatusFailed,
		session.StatusReady,
		session.StatusSuccess,
		session.StatusStopped,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			app := NewApp(nil, nil)
			app.sessions.sessions = []session.Session{
				{ID: "sb-1", Name: "sb", Source: session.SourceSandbox, Status: status, Dir: "/tmp/fake"},
			}
			updated, _ := app.Update(keyPress('s'))
			app = updated.(*App)
			prompt := app.confirmPrompt()
			if !strings.Contains(prompt, "Clean") {
				t.Errorf("prompt = %q, want to contain 'Clean' for sandbox %s", prompt, status)
			}
		})
	}
}

func TestApp_SandboxIsActive(t *testing.T) {
	if !sandboxIsActive(session.StatusSpeccing) {
		t.Error("speccing should be active")
	}
	if !sandboxIsActive(session.StatusRunning) {
		t.Error("running should be active")
	}
	if sandboxIsActive(session.StatusFailed) {
		t.Error("failed should not be active")
	}
	if sandboxIsActive(session.StatusReady) {
		t.Error("ready should not be active")
	}
	if sandboxIsActive(session.StatusSuccess) {
		t.Error("success should not be active")
	}
}

func TestApp_StopConfirmPromptDiffersForStopped(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "stopped-1", Name: "stopped", Source: session.SourceNative, Managed: true, Status: session.StatusStopped},
	}

	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)

	prompt := app.confirmPrompt()
	if !strings.Contains(prompt, "Remove") {
		t.Errorf("prompt = %q, want to contain 'Remove' for stopped session", prompt)
	}
}

func TestApp_ConfirmResumePrompt(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "idle-session", Source: session.SourceNative, Health: session.HealthGreen, Status: session.StatusIdle, Managed: false},
	}

	// Attach on idle non-managed enters confirm resume.
	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)
	if app.confirmAction != confirmResume {
		t.Errorf("confirmAction = %d, want confirmResume", app.confirmAction)
	}

	prompt := app.confirmPrompt()
	if !strings.Contains(prompt, "Resume idle-session") {
		t.Errorf("prompt = %q, want to contain 'Resume idle-session'", prompt)
	}
}

func TestApp_ConfirmNextStagePrompts(t *testing.T) {
	tests := []struct {
		name      string
		status    session.Status
		wantStage string
	}{
		{"ready triggers execute", session.StatusReady, "execute"},
		{"success triggers ship", session.StatusSuccess, "ship"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp(nil, nil)
			app.sessions.sessions = []session.Session{
				{ID: "sb1", Name: "sandbox", Source: session.SourceSandbox, Status: tt.status, Managed: true},
			}

			updated, _ := app.Update(keyPress('a'))
			app = updated.(*App)
			if app.confirmAction != confirmNextStage {
				t.Fatalf("confirmAction = %d, want confirmNextStage", app.confirmAction)
			}

			prompt := app.confirmPrompt()
			if !strings.Contains(prompt, tt.wantStage) {
				t.Errorf("prompt = %q, want to contain %q", prompt, tt.wantStage)
			}
		})
	}
}

func TestApp_ConfirmEscapeCancels(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Source: session.SourceNative, Status: session.StatusRunning, PID: 1},
	}

	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)
	if app.mode != modeConfirm {
		t.Fatal("expected modeConfirm")
	}

	updated, _ = app.Update(keyPress(tea.KeyEscape))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after escape", app.mode)
	}
	if app.confirmAction != confirmNone {
		t.Errorf("confirmAction = %d, want confirmNone", app.confirmAction)
	}
}

func TestApp_LabelInput(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "label-me", Source: session.SourceNative, Health: session.HealthGreen},
	}

	// Enter label mode.
	updated, _ := app.Update(keyPress('l'))
	app = updated.(*App)
	if app.mode != modeLabel {
		t.Fatalf("mode = %d, want modeLabel", app.mode)
	}

	// Type characters.
	for _, c := range "bug fix" {
		updated, _ = app.Update(keyPress(c))
		app = updated.(*App)
	}
	if app.labelInput != "bug fix" {
		t.Errorf("labelInput = %q, want %q", app.labelInput, "bug fix")
	}

	// Backspace removes last char.
	updated, _ = app.Update(keyPress(tea.KeyBackspace))
	app = updated.(*App)
	if app.labelInput != "bug fi" {
		t.Errorf("after backspace: labelInput = %q, want %q", app.labelInput, "bug fi")
	}

	// Escape clears and exits.
	updated, _ = app.Update(keyPress(tea.KeyEscape))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal", app.mode)
	}
	if app.labelInput != "" {
		t.Errorf("labelInput = %q, want empty", app.labelInput)
	}
}

func TestApp_NewSessionPicker_NativeFlow(t *testing.T) {
	app := NewApp(nil, nil)

	// Press `n` to enter new session type picker.
	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	if app.mode != modeNewType {
		t.Fatalf("after n: mode = %d, want modeNewType", app.mode)
	}

	// Pick native with `n`.
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)
	if app.mode != modeNewDir {
		t.Fatalf("after native pick: mode = %d, want modeNewDir", app.mode)
	}
	if app.newSessionKind != newSessionNative {
		t.Errorf("newSessionKind = %d, want newSessionNative", app.newSessionKind)
	}
	if app.newSessionDir == "" {
		t.Error("newSessionDir should be pre-populated with cwd")
	}
}

func TestApp_NewSessionPicker_SandboxFlow(t *testing.T) {
	app := NewApp(nil, nil)

	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)

	updated, _ = app.Update(keyPress('s'))
	app = updated.(*App)
	if app.mode != modeNewDir {
		t.Fatalf("after sandbox pick: mode = %d, want modeNewDir", app.mode)
	}
	if app.newSessionKind != newSessionSandbox {
		t.Errorf("newSessionKind = %d, want newSessionSandbox", app.newSessionKind)
	}
}

func TestApp_NewSessionPicker_EscapeCancels(t *testing.T) {
	app := NewApp(nil, nil)

	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress(tea.KeyEscape))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("after esc from type picker: mode = %d, want modeNormal", app.mode)
	}
}

func TestApp_NewSessionPicker_SandboxRequiresGitRepo(t *testing.T) {
	// A directory that exists but is NOT inside a git repo should be
	// rejected when the user picks sandbox, without reaching the spawn path.
	nonGitDir := t.TempDir()

	app := NewApp(nil, nil)

	// n → s (sandbox) → modeNewDir
	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('s'))
	app = updated.(*App)

	app.newSessionDir = nonGitDir
	updated, _ = app.Update(keyPress(tea.KeyEnter))
	app = updated.(*App)

	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after rejection", app.mode)
	}
	if !strings.Contains(app.flashMsg, "git repo") {
		t.Errorf("flashMsg = %q, want to contain 'git repo'", app.flashMsg)
	}
}

func TestApp_NewSessionPicker_SandboxAcceptsGitRepo(t *testing.T) {
	// A directory inside a git repo should pass validation (we stop
	// before the spawn to avoid needing a real PTY manager).
	gitRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	subdir := filepath.Join(gitRoot, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	if !isGitRepo(subdir) {
		t.Error("isGitRepo(subdir) = false, want true (should walk up to .git)")
	}
}

func TestApp_NewSessionPicker_DirInvalidRejected(t *testing.T) {
	app := NewApp(nil, nil)
	// Nil ptyMgr is fine — the validation happens before spawn.

	// Navigate: n → native → modeNewDir
	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)

	// Replace default cwd with a nonexistent path.
	app.newSessionDir = "/definitely/not/a/real/directory/xyz"
	updated, _ = app.Update(keyPress(tea.KeyEnter))
	app = updated.(*App)

	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after rejection", app.mode)
	}
	if !strings.Contains(app.flashMsg, "Invalid directory") {
		t.Errorf("flashMsg = %q, want to contain 'Invalid directory'", app.flashMsg)
	}
}

func TestApp_NewSessionPicker_DirEmptyRejected(t *testing.T) {
	app := NewApp(nil, nil)

	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)

	// Clear the pre-populated cwd.
	app.newSessionDir = ""
	updated, _ = app.Update(keyPress(tea.KeyEnter))
	app = updated.(*App)

	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after empty rejection", app.mode)
	}
	if !strings.Contains(app.flashMsg, "Directory required") {
		t.Errorf("flashMsg = %q, want to contain 'Directory required'", app.flashMsg)
	}
}

func TestApp_NewSessionPicker_DirEscapeCancels(t *testing.T) {
	app := NewApp(nil, nil)

	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress(tea.KeyEscape))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after esc from dir input", app.mode)
	}
	if app.newSessionDir != "" {
		t.Errorf("newSessionDir = %q, want empty after cancel", app.newSessionDir)
	}
}

func TestApp_NewSessionPicker_DirEditable(t *testing.T) {
	app := NewApp(nil, nil)

	updated, _ := app.Update(keyPress('n'))
	app = updated.(*App)
	updated, _ = app.Update(keyPress('n'))
	app = updated.(*App)

	// Backspace should remove characters from the pre-populated cwd.
	original := app.newSessionDir
	if original == "" {
		t.Fatal("expected pre-populated cwd, got empty")
	}
	updated, _ = app.Update(keyPress(tea.KeyBackspace))
	app = updated.(*App)
	if len(app.newSessionDir) != len(original)-1 {
		t.Errorf("after backspace: len = %d, want %d", len(app.newSessionDir), len(original)-1)
	}
}

func TestApp_WindowSizeMsg(t *testing.T) {
	app := NewApp(nil, nil)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := app.Update(msg)
	app = updated.(*App)

	if app.width != 120 {
		t.Errorf("width = %d, want 120", app.width)
	}
	if app.height != 40 {
		t.Errorf("height = %d, want 40", app.height)
	}
}

func TestApp_FlashMessageExpiry(t *testing.T) {
	app := NewApp(nil, nil)
	app.setFlash("test flash")

	if app.flashMsg != "test flash" {
		t.Errorf("flashMsg = %q, want 'test flash'", app.flashMsg)
	}
	if app.flashExpiry.IsZero() {
		t.Error("flashExpiry should be set")
	}

	// Simulate expiry.
	app.flashExpiry = time.Now().Add(-1 * time.Second)
	msg := tickMsg{sessions: app.sessions.sessions}
	updated, _ := app.Update(msg)
	app = updated.(*App)

	if app.flashMsg != "" {
		t.Errorf("flashMsg = %q, want empty after expiry", app.flashMsg)
	}
}

func TestApp_ExecFinishedMsg(t *testing.T) {
	app := NewApp(nil, nil)

	// After tea.Exec returns, the TUI must force a clear-screen so the
	// alt-screen buffer doesn't retain stale content from the proxy.
	_, cmd := app.Update(execFinishedMsg{err: nil})
	if cmd == nil {
		t.Fatal("execFinishedMsg should return a clear-screen cmd, got nil")
	}
	if cmd() == nil {
		t.Error("clear-screen cmd produced nil msg")
	}
}

func TestApp_AttachNoSelection(t *testing.T) {
	app := NewApp(nil, nil)
	// Empty session list — attach should do nothing.
	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal", app.mode)
	}
}

func TestApp_AttachCannotAttachStatus(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Source: session.SourceSandbox, Status: session.StatusFailed, Managed: false},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)
	if !strings.Contains(app.flashMsg, "Cannot attach") {
		t.Errorf("flash = %q, want to contain 'Cannot attach'", app.flashMsg)
	}
}

func TestApp_StopNoSelection(t *testing.T) {
	app := NewApp(nil, nil)
	// s with empty list should stay in normal mode.
	updated, _ := app.Update(keyPress('s'))
	app = updated.(*App)
	if app.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal", app.mode)
	}
}

func TestReadLogTail_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.jsonl")
	content := []byte(`{"type":"message","timestamp":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := readLogTail(path)
	if err != nil {
		t.Fatalf("readLogTail: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("data = %q, want %q", data, content)
	}
}

func TestReadLogTail_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")

	// Create a file just over 1MB.
	line := strings.Repeat("x", 100) + "\n"
	lineCount := (1024*1024)/len(line) + 100 // ~100 lines over 1MB
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range lineCount {
		fmt.Fprintf(f, "line-%05d:%s", i, line)
	}
	_ = f.Close()

	data, err := readLogTail(path)
	if err != nil {
		t.Fatalf("readLogTail: %v", err)
	}

	// Should be <= 1MB and start at a line boundary.
	if len(data) > 1024*1024 {
		t.Errorf("len(data) = %d, want <= 1MB", len(data))
	}
	// First byte should not be mid-line (the partial line at the seek point is skipped).
	if len(data) > 0 && data[0] == 'x' {
		t.Error("data starts mid-line, expected line boundary")
	}
}

func TestReadLogTail_Nonexistent(t *testing.T) {
	_, err := readLogTail("/nonexistent/path.jsonl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestApp_ViewWithDimensions(t *testing.T) {
	app := NewApp(nil, nil)
	app.width = 100
	app.height = 30
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "test", Source: session.SourceNative, Health: session.HealthGreen, Status: session.StatusRunning},
	}

	view := app.View()
	if view.AltScreen != true {
		t.Error("View should request alt screen")
	}
	if !strings.Contains(view.Content, "Sessions") {
		t.Errorf("view missing 'Sessions' pane title")
	}
	if !strings.Contains(view.Content, "test") {
		t.Errorf("view missing selected session name in detail pane title")
	}
}

func TestApp_ViewZeroDimensions(t *testing.T) {
	app := NewApp(nil, nil)
	view := app.View()
	if !strings.Contains(view.Content, "Loading") {
		t.Errorf("zero-size view = %q, want 'Loading...'", view.Content)
	}
}
