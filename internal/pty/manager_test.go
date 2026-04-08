package pty

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestManager_Spawn(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	// Long-running command so the session doesn't auto-cleanup
	// before the subtests inspect manager state.
	cmd := exec.Command("sleep", "30")
	err := m.Spawn(t.Context(), "test-1", cmd, "/tmp", session.SourceNative)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), "test-1") })

	t.Run("session is retrievable", func(t *testing.T) {
		sess, ok := m.Get("test-1")
		if !ok {
			t.Fatal("Get returned false for spawned session")
		}
		if sess.ID != "test-1" {
			t.Errorf("ID = %q, want %q", sess.ID, "test-1")
		}
		if sess.started.IsZero() {
			t.Error("started time is zero")
		}
	})

	t.Run("metadata file written", func(t *testing.T) {
		metaPath := filepath.Join(stateDir, "test-1.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("read metadata: %v", err)
		}
		var meta session.ManagedMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta.ID != "test-1" {
			t.Errorf("metadata ID = %q, want %q", meta.ID, "test-1")
		}
		if meta.Dir != "/tmp" {
			t.Errorf("metadata Dir = %q, want %q", meta.Dir, "/tmp")
		}
		if !meta.Managed {
			t.Error("metadata Managed = false, want true")
		}
		if meta.PID == 0 {
			t.Error("metadata PID = 0, want nonzero")
		}
	})

	t.Run("duplicate ID rejected", func(t *testing.T) {
		cmd := exec.Command("echo", "dup")
		err := m.Spawn(t.Context(), "test-1", cmd, "/tmp", session.SourceNative)
		if err == nil {
			t.Fatal("expected error for duplicate ID")
		}
	})
}

func TestManager_SpawnAndWaitForExit(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "done")
	err := m.Spawn(t.Context(), "exit-test", cmd, "/tmp", session.SourceNative)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), "exit-test") })

	sess, ok := m.Get("exit-test")
	if !ok {
		t.Fatal("Get returned false")
	}

	select {
	case <-sess.Done:
		// process exited as expected
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestManager_Stop(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("sleep", "60")
	err := m.Spawn(t.Context(), "stop-test", cmd, "/tmp", session.SourceNative)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	err = m.Stop(t.Context(), "stop-test")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	t.Run("session removed from map", func(t *testing.T) {
		_, ok := m.Get("stop-test")
		if ok {
			t.Error("Get returned true after Stop")
		}
	})

	t.Run("metadata file preserved after stop", func(t *testing.T) {
		// Stop kills the process but keeps the metadata so the session
		// remains visible as "stopped" and can be resumed.
		metaPath := filepath.Join(stateDir, "stop-test.json")
		if _, err := os.Stat(metaPath); err != nil {
			t.Errorf("metadata file should persist after Stop: %v", err)
		}
	})

	t.Run("stop nonexistent returns error", func(t *testing.T) {
		err := m.Stop(t.Context(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestManager_Get_Nonexistent(t *testing.T) {
	m := NewManager(t.TempDir())

	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("Get returned true for nonexistent session")
	}
}

func TestManager_AutoCleanupOnProcessExit(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "quick")
	if err := m.Spawn(t.Context(), "auto-cleanup", cmd, "/tmp", session.SourceNative); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	sess, ok := m.Get("auto-cleanup")
	if !ok {
		t.Fatal("session not in map immediately after Spawn")
	}

	select {
	case <-sess.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}

	// Give the cleanup goroutine a moment to run after Done closes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.Get("auto-cleanup"); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("session removed from map after exit", func(t *testing.T) {
		if _, ok := m.Get("auto-cleanup"); ok {
			t.Error("session still in map after process exit")
		}
	})

	t.Run("metadata file preserved after natural exit", func(t *testing.T) {
		// Process exiting on its own leaves the session as "stopped"
		// so the user can resume it. Explicit removal requires a
		// separate call.
		metaPath := filepath.Join(stateDir, "auto-cleanup.json")
		if _, err := os.Stat(metaPath); err != nil {
			t.Errorf("metadata file should persist after process exit: %v", err)
		}
	})
}

func TestManager_RemoveMetadata(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	// Simulate an orphaned metadata file from a previous cs instance.
	metaPath := filepath.Join(stateDir, "orphan.json")
	if err := os.WriteFile(metaPath, []byte(`{"id":"orphan"}`), 0o644); err != nil {
		t.Fatalf("seed orphan metadata: %v", err)
	}

	if err := m.RemoveMetadata("orphan"); err != nil {
		t.Fatalf("RemoveMetadata: %v", err)
	}

	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("orphan metadata file still present after RemoveMetadata")
	}

	// Idempotent: removing nonexistent should not error.
	if err := m.RemoveMetadata("nonexistent"); err != nil {
		t.Errorf("RemoveMetadata on nonexistent: %v", err)
	}
}
