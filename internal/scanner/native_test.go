package scanner

import (
	"context"
	"os"
	"path/filepath"
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
	})

	t.Run("status reflects process liveness", func(t *testing.T) {
		if s.Status != session.StatusIdle {
			t.Errorf("Status = %s, want idle (stale PID)", s.Status)
		}
	})
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
