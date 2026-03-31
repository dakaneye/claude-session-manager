package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestNativeSource_Scan(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "native", "sessions", "12345.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "12345.json"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &NativeSource{ClaudeDir: tmpDir}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("discovers session", func(t *testing.T) {
		if len(sessions) != 1 {
			t.Fatalf("len(sessions) = %d, want 1", len(sessions))
		}
	})

	s := sessions[0]

	t.Run("session fields", func(t *testing.T) {
		if s.ID != "578bd126-4b4b-43ff-aba6-88d872b0cc27" {
			t.Errorf("ID = %s, want 578bd126-...", s.ID)
		}
		if s.Source != session.SourceNative {
			t.Errorf("Source = %s, want native", s.Source)
		}
		if s.Dir != "/Users/test/dev/myproject" {
			t.Errorf("Dir = %s, want /Users/test/dev/myproject", s.Dir)
		}
		if s.PID != 12345 {
			t.Errorf("PID = %d, want 12345", s.PID)
		}
		if s.Name != "myproject" {
			t.Errorf("Name = %q, want %q", s.Name, "myproject")
		}
		if s.Task != "" {
			t.Errorf("Task = %q, want empty", s.Task)
		}
	})

	t.Run("status reflects process liveness", func(t *testing.T) {
		if s.Status != session.StatusIdle {
			t.Errorf("Status = %s, want idle (stale PID)", s.Status)
		}
	})
}

func TestNativeLogPath(t *testing.T) {
	got := nativeLogPath("/home/user/.claude", "/Users/test/dev/myproject", "abc-123")
	want := "/home/user/.claude/projects/-Users-test-dev-myproject/abc-123.jsonl"
	if got != want {
		t.Errorf("nativeLogPath = %q, want %q", got, want)
	}
}

func TestNativeSource_ParsesActivityFromJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-session-id"
	cwd := "/Users/test/dev/myproject"

	// Write session file.
	sessJSON := fmt.Sprintf(`{"pid":99999999,"sessionId":%q,"cwd":%q,"startedAt":1774912561112,"kind":"interactive","entrypoint":"cli"}`,
		sessionID, cwd)
	if err := os.WriteFile(filepath.Join(sessDir, "99999999.json"), []byte(sessJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write JSONL log file.
	encoded := strings.ReplaceAll(cwd, "/", "-")
	projectDir := filepath.Join(tmpDir, "projects", encoded)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logContent := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/workspace/main.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]},"timestamp":"2026-03-27T14:30:00.000Z"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	src := &NativeSource{ClaudeDir: tmpDir}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}

	s := sessions[0]
	if s.LogPath == "" {
		t.Error("LogPath should be set when JSONL exists")
	}
	if s.LastActivity.IsZero() {
		t.Error("LastActivity should be set from parsed JSONL")
	}
}

func TestNativeSource_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	src := &NativeSource{ClaudeDir: tmpDir}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}
