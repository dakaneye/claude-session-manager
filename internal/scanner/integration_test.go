package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestIntegration_FullScan(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up sandbox session.
	sandboxDir := filepath.Join(tmpDir, "repo")
	sessDir := filepath.Join(sandboxDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sandboxJSON := map[string]any{
		"id":            "2026-03-27-test01",
		"name":          "integration-test",
		"worktree_path": sandboxDir,
		"branch":        "sandbox/2026-03-27-test01",
		"status":        "running",
		"log_path":      "",
		"created_at":    "2026-03-27T14:00:00Z",
		"started_at":    "2026-03-27T14:30:00Z",
	}
	data, _ := json.MarshalIndent(sandboxJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(sessDir, "2026-03-27-test01.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up log for sandbox session.
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logFixture, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "logs", "2026-03-27-abc123.log"))
	if err := os.WriteFile(filepath.Join(logDir, "2026-03-27-test01.log"), logFixture, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up PLAN.md in worktree.
	if err := os.WriteFile(filepath.Join(sandboxDir, "PLAN.md"), []byte("# Refactor auth middleware\n\nSome plan."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up native session.
	claudeDir := filepath.Join(tmpDir, "claude")
	nativeSessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(nativeSessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeJSON := map[string]any{
		"pid":        99999999,
		"sessionId":  "native-test-uuid",
		"cwd":        "/tmp/test-project",
		"startedAt":  1774912561112,
		"kind":       "interactive",
		"entrypoint": "cli",
	}
	nativeData, _ := json.MarshalIndent(nativeJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(nativeSessDir, "99999999.json"), nativeData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Build scanner with both sources.
	sc := &Scanner{
		Sources: []SessionSource{
			&SandboxSource{
				RepoPaths: []string{sandboxDir},
				LogDir:    logDir,
			},
			&NativeSource{
				ClaudeDir: claudeDir,
			},
		},
	}

	sessions, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("finds both session types", func(t *testing.T) {
		if len(sessions) != 2 {
			t.Fatalf("len(sessions) = %d, want 2", len(sessions))
		}
	})

	var sandbox, native *session.Session
	for i := range sessions {
		switch sessions[i].Source {
		case session.SourceSandbox:
			sandbox = &sessions[i]
		case session.SourceNative:
			native = &sessions[i]
		}
	}

	t.Run("sandbox session fully populated", func(t *testing.T) {
		if sandbox == nil {
			t.Fatal("sandbox session not found")
		}
		if sandbox.ID != "2026-03-27-test01" {
			t.Errorf("ID = %s", sandbox.ID)
		}
		if sandbox.Name != "integration-test" {
			t.Errorf("Name = %s", sandbox.Name)
		}
		if sandbox.Status != session.StatusRunning {
			t.Errorf("Status = %s", sandbox.Status)
		}
		if sandbox.Task != "Refactor auth middleware" {
			t.Errorf("Task = %q, want 'Refactor auth middleware'", sandbox.Task)
		}
		if sandbox.LastActivity.IsZero() {
			t.Error("LastActivity is zero, expected parsed from log")
		}
	})

	t.Run("native session populated", func(t *testing.T) {
		if native == nil {
			t.Fatal("native session not found")
		}
		if native.ID != "native-test-uuid" {
			t.Errorf("ID = %s", native.ID)
		}
		if native.Status != session.StatusIdle {
			t.Errorf("Status = %s, want idle (stale PID)", native.Status)
		}
		if native.Dir != "/tmp/test-project" {
			t.Errorf("Dir = %s", native.Dir)
		}
	})

	t.Run("sandbox session has health from log", func(t *testing.T) {
		if sandbox == nil {
			t.Skip("no sandbox session")
		}
		// The fixture log has 2 edits to the same file (under 5 threshold) and
		// ends with a passing test. However, the same Bash command is run 3
		// consecutive times (2 failures then 1 pass), which triggers the
		// repeated-command heuristic at warning severity. The fixture timestamps
		// are in the past (2026-03-27), so idle detection also fires critical.
		// Health is therefore red.
		if sandbox.Health != session.HealthRed {
			t.Errorf("Health = %s, want red (idle critical from stale fixture timestamps)", sandbox.Health)
		}
	})
}

func TestIntegration_ManagedSessionDeduplication(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a native session.
	claudeDir := filepath.Join(tmpDir, "claude")
	nativeSessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(nativeSessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeJSON := map[string]any{
		"pid":       88888888,
		"sessionId": "dedup-native",
		"cwd":       "/tmp/dedup-project",
		"startedAt": 1774912561112,
	}
	data, _ := json.MarshalIndent(nativeJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(nativeSessDir, "88888888.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up a managed session with the same PID.
	managedDir := filepath.Join(tmpDir, "cs-sessions")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedJSON := map[string]any{
		"id":         "dedup-managed",
		"pid":        88888888,
		"dir":        "/tmp/dedup-project",
		"source":     "native",
		"created_at": "2026-03-31T10:00:00Z",
		"managed":    true,
	}
	mdata, _ := json.MarshalIndent(managedJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(managedDir, "dedup-managed.json"), mdata, 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &Scanner{
		Sources: []SessionSource{
			&ManagedSource{StateDir: managedDir},
			&NativeSource{ClaudeDir: claudeDir},
		},
	}

	sessions, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	pidCount := 0
	for _, s := range sessions {
		if s.PID == 88888888 {
			pidCount++
			if !s.Managed {
				t.Error("deduped session should be the managed one")
			}
		}
	}

	if pidCount != 1 {
		t.Errorf("PID 88888888 appears %d times, want 1", pidCount)
	}
}
