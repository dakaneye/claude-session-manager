package pty

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestManager_Spawn(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "hello")
	err := m.Spawn("test-1", cmd, "/tmp")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop("test-1") })

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
		var meta Metadata
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
		err := m.Spawn("test-1", cmd, "/tmp")
		if err == nil {
			t.Fatal("expected error for duplicate ID")
		}
	})
}

func TestManager_SpawnAndWaitForExit(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "done")
	err := m.Spawn("exit-test", cmd, "/tmp")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop("exit-test") })

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
	err := m.Spawn("stop-test", cmd, "/tmp")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	err = m.Stop("stop-test")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	t.Run("session removed from map", func(t *testing.T) {
		_, ok := m.Get("stop-test")
		if ok {
			t.Error("Get returned true after Stop")
		}
	})

	t.Run("metadata file removed", func(t *testing.T) {
		metaPath := filepath.Join(stateDir, "stop-test.json")
		if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
			t.Errorf("metadata file still exists after Stop")
		}
	})

	t.Run("stop nonexistent returns error", func(t *testing.T) {
		err := m.Stop("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestManager_List(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	t.Run("empty manager", func(t *testing.T) {
		ids := m.List()
		if len(ids) != 0 {
			t.Errorf("List() = %v, want empty", ids)
		}
	})

	cmd1 := exec.Command("sleep", "60")
	if err := m.Spawn("list-1", cmd1, "/tmp"); err != nil {
		t.Fatalf("Spawn list-1: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop("list-1") })

	cmd2 := exec.Command("sleep", "60")
	if err := m.Spawn("list-2", cmd2, "/tmp"); err != nil {
		t.Fatalf("Spawn list-2: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop("list-2") })

	t.Run("lists all sessions", func(t *testing.T) {
		ids := m.List()
		if len(ids) != 2 {
			t.Fatalf("List() len = %d, want 2", len(ids))
		}
		found := map[string]bool{}
		for _, id := range ids {
			found[id] = true
		}
		if !found["list-1"] || !found["list-2"] {
			t.Errorf("List() = %v, want list-1 and list-2", ids)
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
