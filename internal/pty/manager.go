package pty

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/xpty"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

// ManagedSession represents a PTY-backed session managed by the Manager.
type ManagedSession struct {
	ID      string
	Cmd     *exec.Cmd
	Pty     xpty.Pty
	Done    chan struct{}
	dir     string
	source  session.Source
	started time.Time
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
func (m *Manager) Spawn(ctx context.Context, id string, cmd *exec.Cmd, dir string, source session.Source) error {
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
		source:  source,
		started: now,
	}

	go func() {
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			<-done
		}
		// Process exited: close PTY and remove from in-memory map.
		// Metadata stays on disk so the session remains visible as
		// "stopped" and the user can resume it via `claude --resume`.
		_ = p.Close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		close(sess.Done)
	}()

	m.sessions[id] = sess

	if err := m.writeMetadata(sess); err != nil {
		delete(m.sessions, id)
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		<-sess.Done // Wait for process exit before closing PTY.
		_ = p.Close()
		return fmt.Errorf("persist metadata: %w", err)
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

// Stop sends SIGTERM to the session's process, closes the PTY, and
// removes it from the in-memory map. The metadata file is preserved
// so the session remains visible as "stopped" and can be resumed.
// Returns an error only if the session is unknown to this Manager.
func (m *Manager) Stop(_ context.Context, id string) error {
	sess, err := m.remove(id)
	if err != nil {
		return err
	}

	if sess.Cmd.Process != nil {
		_ = sess.Cmd.Process.Signal(syscall.SIGTERM)
	}

	_ = sess.Pty.Close()

	return nil
}

func (m *Manager) remove(id string) (*ManagedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	delete(m.sessions, id)
	return sess, nil
}

// RemoveMetadata deletes the on-disk metadata file for id.
// Used to clean up orphaned sessions discovered from disk that
// this Manager doesn't own in memory.
func (m *Manager) RemoveMetadata(id string) error {
	return m.removeMetadata(id)
}

func (m *Manager) writeMetadata(sess *ManagedSession) error {
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	meta := session.ManagedMeta{
		ID:        sess.ID,
		PID:       sess.Cmd.Process.Pid,
		Dir:       sess.dir,
		Source:    sess.source,
		CreatedAt: sess.started,
		Managed:   true,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	path := filepath.Join(m.stateDir, sess.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

func (m *Manager) removeMetadata(id string) error {
	path := filepath.Join(m.stateDir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
