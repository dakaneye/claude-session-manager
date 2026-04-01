package pty

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/xpty"
)

// ManagedSession represents a PTY-backed session managed by the Manager.
type ManagedSession struct {
	ID      string
	Cmd     *exec.Cmd
	Pty     xpty.Pty
	Done    chan struct{}
	dir     string
	started time.Time
}

// Metadata is the on-disk representation of a managed session.
type Metadata struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Dir       string    `json:"dir"`
	Source    string    `json:"source"`
	Stage     string    `json:"stage,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Managed   bool      `json:"managed"`
}

// Manager tracks and controls PTY-backed sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*ManagedSession
	stateDir string
}

// NewManager creates a Manager that persists metadata to stateDir.
func NewManager(stateDir string) *Manager {
	return &Manager{
		sessions: make(map[string]*ManagedSession),
		stateDir: stateDir,
	}
}

// Spawn allocates a PTY, starts cmd, and tracks the session under id.
// dir is the working directory associated with the session.
func (m *Manager) Spawn(id string, cmd *exec.Cmd, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session %q already exists", id)
	}

	p, err := xpty.NewPty(80, 24)
	if err != nil {
		return fmt.Errorf("allocate pty: %w", err)
	}

	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		return fmt.Errorf("start command: %w", err)
	}

	now := time.Now()
	sess := &ManagedSession{
		ID:      id,
		Cmd:     cmd,
		Pty:     p,
		Done:    make(chan struct{}),
		dir:     dir,
		started: now,
	}

	// Monitor process exit in the background.
	go func() {
		_ = cmd.Wait()
		close(sess.Done)
	}()

	m.sessions[id] = sess

	meta := Metadata{
		ID:        id,
		PID:       cmd.Process.Pid,
		Dir:       dir,
		Source:    "managed",
		CreatedAt: now,
		Managed:   true,
	}
	if err := m.writeMetadata(meta); err != nil {
		// Best-effort: session is running but metadata failed to persist.
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

// Get returns the ManagedSession for id, or false if not found.
func (m *Manager) Get(id string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	return sess, ok
}

// Stop sends SIGTERM to the session's process, closes the PTY,
// removes it from the map, and deletes the metadata file.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	// Signal the process to exit.
	if sess.Cmd.Process != nil {
		_ = sess.Cmd.Process.Signal(syscall.SIGTERM)
	}

	_ = sess.Pty.Close()
	_ = m.removeMetadata(id)

	return nil
}

// List returns the IDs of all active managed sessions.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) writeMetadata(meta Metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	path := filepath.Join(m.stateDir, meta.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (m *Manager) removeMetadata(id string) error {
	path := filepath.Join(m.stateDir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
