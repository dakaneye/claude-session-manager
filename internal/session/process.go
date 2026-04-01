package session

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// IsProcessAlive checks whether a process with the given PID is running.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// StopProcess sends SIGTERM to the process with the given PID.
func StopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}

// DefaultStateDir returns the path to the cs-managed sessions directory.
func DefaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "cs-sessions"), nil
}

// ManagedMetaPath returns the metadata file path for a managed session.
func ManagedMetaPath(id string) (string, error) {
	dir, err := DefaultStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}
