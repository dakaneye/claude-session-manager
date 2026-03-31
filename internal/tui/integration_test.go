package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

// TestIntegration_ScannerToTUI creates real fixture files, scans them,
// feeds the results to the TUI, and verifies the rendered output.
func TestIntegration_ScannerToTUI(t *testing.T) {
	tmpDir := t.TempDir()

	// --- Create sandbox session fixtures ---
	sandboxDir := filepath.Join(tmpDir, "repo")
	sessDir := filepath.Join(sandboxDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sandboxJSON := map[string]any{
		"id":            "2026-03-27-integ1",
		"name":          "feature-auth",
		"worktree_path": sandboxDir,
		"branch":        "sandbox/2026-03-27-integ1",
		"status":        "running",
		"log_path":      "",
		"created_at":    "2026-03-27T14:00:00Z",
		"started_at":    "2026-03-27T14:30:00Z",
	}
	data, err := json.MarshalIndent(sandboxJSON, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "2026-03-27-integ1.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a PLAN.md for task detection.
	if err := os.WriteFile(filepath.Join(sandboxDir, "PLAN.md"), []byte("# Implement OAuth2 flow\n\nDetails here."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a log file with activity.
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logContent := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"integ1"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/workspace/main.go"}}]},"session_id":"integ1"}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"package main"}]},"timestamp":"2026-03-27T14:30:00.000Z","session_id":"integ1"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/workspace/auth/oauth.go","old_string":"a","new_string":"b"}}]},"session_id":"integ1"}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"OK"}]},"timestamp":"2026-03-27T14:31:00.000Z","session_id":"integ1"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "2026-03-27-integ1.log"), []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Create native session fixture ---
	claudeDir := filepath.Join(tmpDir, "claude")
	nativeSessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(nativeSessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeJSON := map[string]any{
		"pid":        88888888,
		"sessionId":  "native-integ-uuid",
		"cwd":        "/home/user/dev/api-server",
		"startedAt":  1774912561112,
		"kind":       "interactive",
		"entrypoint": "cli",
	}
	nativeData, err := json.MarshalIndent(nativeJSON, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeSessDir, "88888888.json"), nativeData, 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Run the scanner ---
	sc := &scanner.Scanner{
		Sources: []scanner.SessionSource{
			&scanner.SandboxSource{
				RepoPaths: []string{sandboxDir},
				LogDir:    logDir,
			},
			&scanner.NativeSource{
				ClaudeDir: claudeDir,
			},
		},
	}

	sessions, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(sessions) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(sessions))
	}

	// Parse log for activity entries.
	logData, err := os.ReadFile(filepath.Join(logDir, "2026-03-27-integ1.log"))
	if err != nil {
		t.Fatal(err)
	}
	summary := scanner.ParseLog(logData)

	activities := map[string][]scanner.ActivityEntry{
		"2026-03-27-integ1": summary.RecentActivity,
	}

	// --- Feed to TUI and verify rendering ---
	app := NewApp(nil)
	app.width = 120
	app.height = 40
	app.sessions.SetSessions(sessions)
	app.activities = activities
	app.updateDetail()

	// Set component sizes as App.View() would.
	listWidth := app.width * 30 / 100
	detailWidth := app.width - listWidth
	contentHeight := app.height - 2
	app.sessions.SetSize(listWidth-2, contentHeight-2)
	app.detail.SetSize(detailWidth-2, contentHeight-2)

	listView := app.sessions.View()
	detailView := app.detail.View()

	// --- Assert session list contains both sessions ---
	t.Run("list shows sandbox session", func(t *testing.T) {
		if !strings.Contains(listView, "feature-auth") {
			t.Errorf("sandbox session 'feature-auth' not in list:\n%s", listView)
		}
	})

	t.Run("list shows native session", func(t *testing.T) {
		// Native session has no name; should show ID or directory basename.
		if !strings.Contains(listView, "native-integ-uuid") && !strings.Contains(listView, "api-server") {
			t.Errorf("native session not in list:\n%s", listView)
		}
	})

	t.Run("list shows source types", func(t *testing.T) {
		if !strings.Contains(listView, "sandbox") {
			t.Error("missing sandbox source in list")
		}
		if !strings.Contains(listView, "native") {
			t.Error("missing native source in list")
		}
	})

	// --- Assert detail pane shows selected session ---
	selected := app.sessions.Selected()
	if selected == nil {
		t.Fatal("no session selected")
	}

	t.Run("detail shows selected session directory", func(t *testing.T) {
		if !strings.Contains(detailView, selected.Dir) {
			t.Errorf("detail missing directory %q:\n%s", selected.Dir, detailView)
		}
	})

	// Navigate to sandbox session and verify detail.
	for i, s := range sessions {
		if s.Source == session.SourceSandbox {
			app.sessions.cursor = i
			app.updateDetail()
			app.detail.SetSize(detailWidth-2, contentHeight-2)
			break
		}
	}

	sandboxDetail := app.detail.View()

	t.Run("sandbox detail shows task from PLAN.md", func(t *testing.T) {
		if !strings.Contains(sandboxDetail, "Implement OAuth2 flow") {
			t.Errorf("missing task from PLAN.md:\n%s", sandboxDetail)
		}
	})

	t.Run("sandbox detail shows activity from log", func(t *testing.T) {
		if !strings.Contains(sandboxDetail, "Recent Activity") {
			t.Error("missing Recent Activity section")
		}
		if !strings.Contains(sandboxDetail, "Read") {
			t.Error("missing Read tool in activity")
		}
		if !strings.Contains(sandboxDetail, "Edit") {
			t.Error("missing Edit tool in activity")
		}
	})

	t.Run("sandbox detail shows file paths from activity", func(t *testing.T) {
		// detail.go uses filepath.Base on the Detail field.
		if !strings.Contains(sandboxDetail, "main.go") {
			t.Error("missing main.go in activity")
		}
		if !strings.Contains(sandboxDetail, "oauth.go") {
			t.Error("missing oauth.go in activity")
		}
	})

	// --- Verify App.View() does not panic ---
	t.Run("full App.View does not panic", func(t *testing.T) {
		view := app.View()
		_ = view
	})
}
