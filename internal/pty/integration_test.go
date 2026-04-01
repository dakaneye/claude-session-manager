package pty

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegration_SpawnWritesMetadata(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "cs-sessions")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(stateDir)

	cmd := exec.Command("sleep", "10")
	if err := mgr.Spawn("int-test-1", cmd, "/tmp"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("int-test-1") })

	metaPath := filepath.Join(stateDir, "int-test-1.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	if meta.ID != "int-test-1" {
		t.Errorf("ID = %q", meta.ID)
	}
	if meta.PID == 0 {
		t.Error("PID = 0, want nonzero")
	}
	if !meta.Managed {
		t.Error("Managed = false")
	}
}

func TestIntegration_StopCleansMetadata(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "cs-sessions")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(stateDir)

	cmd := exec.Command("sleep", "60")
	if err := mgr.Spawn("stop-test", cmd, "/tmp"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	metaPath := filepath.Join(stateDir, "stop-test.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("metadata should exist: %v", err)
	}

	if err := mgr.Stop("stop-test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("metadata should be removed after stop")
	}
}

func TestIntegration_ProcessExitClosesDone(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "cs-sessions")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(stateDir)

	cmd := exec.Command("true")
	if err := mgr.Spawn("exit-test", cmd, "/tmp"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	sess, ok := mgr.Get("exit-test")
	if !ok {
		t.Fatal("session not found")
	}

	select {
	case <-sess.Done:
		// Expected.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Done channel")
	}
}
