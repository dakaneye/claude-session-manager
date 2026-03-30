package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestSandboxSource_Scan(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "sessions", "2026-03-27-abc123.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "2026-03-27-abc123.json"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	logFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "logs", "2026-03-27-abc123.log"))
	if err != nil {
		t.Fatalf("read log fixture: %v", err)
	}
	logDir := filepath.Join(tmpDir, "sandbox-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "2026-03-27-abc123.log")
	if err := os.WriteFile(logPath, logFixture, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &SandboxSource{
		RepoPaths: []string{tmpDir},
		LogDir:    logDir,
	}

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
		if s.ID != "2026-03-27-abc123" {
			t.Errorf("ID = %s, want 2026-03-27-abc123", s.ID)
		}
		if s.Name != "auth-refactor" {
			t.Errorf("Name = %s, want auth-refactor", s.Name)
		}
		if s.Source != session.SourceSandbox {
			t.Errorf("Source = %s, want sandbox", s.Source)
		}
		if s.Status != session.StatusRunning {
			t.Errorf("Status = %s, want running", s.Status)
		}
	})

	t.Run("log parsed for activity", func(t *testing.T) {
		if s.LastActivity.IsZero() {
			t.Error("LastActivity is zero, expected parsed from log")
		}
	})
}

func TestSandboxSource_SkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "sessions", "2026-03-27-abc123.json"))
	realFile := filepath.Join(sessDir, "2026-03-27-abc123.json")
	os.WriteFile(realFile, fixture, 0o644)
	os.Symlink(realFile, filepath.Join(sessDir, "auth-refactor.json"))

	src := &SandboxSource{RepoPaths: []string{tmpDir}}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 {
		t.Errorf("len(sessions) = %d, want 1 (symlink should be skipped)", len(sessions))
	}
}
