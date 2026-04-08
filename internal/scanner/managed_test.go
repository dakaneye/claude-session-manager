package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestManagedSource_Scan(t *testing.T) {
	t.Run("returns stopped session for dead PID", func(t *testing.T) {
		dir := t.TempDir()

		meta := session.ManagedMeta{
			ID:        "test-session-1",
			PID:       99999999,
			Dir:       "/home/user/project",
			Source:    session.SourceNative,
			CreatedAt: time.Now().Add(-5 * time.Minute),
			Managed:   true,
		}
		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("marshal meta: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "test-session-1.json"), data, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		src := &ManagedSource{StateDir: dir}
		sessions, err := src.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		if len(sessions) != 1 {
			t.Fatalf("len(sessions) = %d, want 1", len(sessions))
		}

		s := sessions[0]
		if s.ID != "test-session-1" {
			t.Errorf("ID = %q, want %q", s.ID, "test-session-1")
		}
		if s.Status != session.StatusStopped {
			t.Errorf("Status = %q, want %q", s.Status, session.StatusStopped)
		}
		if !s.Managed {
			t.Error("Managed = false, want true")
		}
		if s.PID != 99999999 {
			t.Errorf("PID = %d, want 99999999", s.PID)
		}
	})

	t.Run("nonexistent directory returns empty slice", func(t *testing.T) {
		src := &ManagedSource{StateDir: "/tmp/cs-sessions-does-not-exist-12345"}
		sessions, err := src.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("len(sessions) = %d, want 0", len(sessions))
		}
	})

	t.Run("empty directory returns empty slice", func(t *testing.T) {
		dir := t.TempDir()
		src := &ManagedSource{StateDir: dir}
		sessions, err := src.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("len(sessions) = %d, want 0", len(sessions))
		}
	})

	t.Run("defaults empty source to native", func(t *testing.T) {
		dir := t.TempDir()

		meta := session.ManagedMeta{
			ID:        "no-source-session",
			PID:       99999999,
			Dir:       "/home/user/project",
			Source:    "",
			CreatedAt: time.Now(),
			Managed:   true,
		}
		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("marshal meta: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "no-source-session.json"), data, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		src := &ManagedSource{StateDir: dir}
		sessions, err := src.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("len(sessions) = %d, want 1", len(sessions))
		}
		if sessions[0].Source != session.SourceNative {
			t.Errorf("Source = %q, want %q", sessions[0].Source, session.SourceNative)
		}
	})
}
