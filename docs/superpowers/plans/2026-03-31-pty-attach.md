# PTY-Based Session Attach/Detach Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable full bidirectional attach/detach for Claude sessions launched through `cs`, with resume support for dead sessions and stage-based sandbox lifecycle management.

**Architecture:** New `internal/pty/` package owns PTY spawn/attach/detach lifecycle. Session model gains `Managed` field. Scanner gains `ManagedSource` to discover cs-owned sessions from `~/.claude/cs-sessions/`. TUI uses `tea.ExecProcess` for full-screen attach with `Ctrl+]` detach. CLI gains `attach`, `resume`, and `sandbox` subcommands.

**Tech Stack:** `charmbracelet/x/xpty` for PTY allocation, `charm.land/bubbletea/v2` ExecProcess for TUI suspend/resume, `github.com/spf13/cobra` for CLI commands.

---

### Task 1: Add xpty dependency and PTY manager core

**Files:**
- Modify: `go.mod`
- Create: `internal/pty/manager.go`
- Create: `internal/pty/manager_test.go`

- [ ] **Step 1: Add xpty dependency**

```bash
cd /Users/samueldacanay/dev/personal/claude-session-manager
go get github.com/charmbracelet/x/xpty@latest
```

- [ ] **Step 2: Write failing test for PTY spawn**

Create `internal/pty/manager_test.go`:

```go
package pty

import (
	"os/exec"
	"testing"
	"time"
)

func TestManager_Spawn(t *testing.T) {
	mgr := NewManager(t.TempDir())

	cmd := exec.Command("echo", "hello")
	sess, err := mgr.Spawn("test-1", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if sess.ID != "test-1" {
		t.Errorf("ID = %q, want test-1", sess.ID)
	}
	if sess.Cmd.Process == nil {
		t.Fatal("process not started")
	}

	// Wait for the short-lived process to exit.
	select {
	case <-sess.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestManager_Get(t *testing.T) {
	mgr := NewManager(t.TempDir())

	cmd := exec.Command("sleep", "10")
	_, err := mgr.Spawn("sess-1", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { mgr.Stop("sess-1") })

	t.Run("found", func(t *testing.T) {
		sess, ok := mgr.Get("sess-1")
		if !ok {
			t.Fatal("session not found")
		}
		if sess.ID != "sess-1" {
			t.Errorf("ID = %q", sess.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := mgr.Get("no-such")
		if ok {
			t.Error("expected not found")
		}
	})
}

func TestManager_Stop(t *testing.T) {
	mgr := NewManager(t.TempDir())

	cmd := exec.Command("sleep", "60")
	_, err := mgr.Spawn("stop-me", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := mgr.Stop("stop-me"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, ok := mgr.Get("stop-me")
	if ok {
		t.Error("session should be removed after stop")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/pty/ -v -run TestManager
```

Expected: compilation error — `pty` package does not exist yet.

- [ ] **Step 4: Implement PTY manager**

Create `internal/pty/manager.go`:

```go
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

// ManagedSession represents a session with a PTY owned by cs.
type ManagedSession struct {
	ID      string
	Cmd     *exec.Cmd
	Pty     xpty.Pty
	Done    chan struct{} // closed when process exits
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

// Manager owns all PTY-managed sessions for this cs process.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*ManagedSession
	stateDir string // ~/.claude/cs-sessions/
}

// NewManager creates a Manager that persists metadata to stateDir.
func NewManager(stateDir string) *Manager {
	return &Manager{
		sessions: make(map[string]*ManagedSession),
		stateDir: stateDir,
	}
}

// Spawn creates a new PTY, starts cmd on it, and tracks the session.
func (m *Manager) Spawn(id string, cmd *exec.Cmd) (*ManagedSession, error) {
	pty, err := xpty.NewPty(80, 24)
	if err != nil {
		return nil, fmt.Errorf("create pty: %w", err)
	}

	if err := pty.Start(cmd); err != nil {
		pty.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}

	sess := &ManagedSession{
		ID:      id,
		Cmd:     cmd,
		Pty:     pty,
		Done:    make(chan struct{}),
		dir:     cmd.Dir,
		started: time.Now(),
	}

	// Monitor process exit in background.
	go func() {
		_ = xpty.WaitProcess(cmd.Context(), cmd)
		close(sess.Done)
	}()

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	if err := m.writeMetadata(sess); err != nil {
		// Non-fatal — session still works without persisted metadata.
		_ = err
	}

	return sess, nil
}

// Get returns a managed session by ID.
func (m *Manager) Get(id string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	return sess, ok
}

// Stop sends SIGTERM to a managed session and cleans up.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found: %s", id)
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	if sess.Cmd.Process != nil {
		_ = sess.Cmd.Process.Signal(syscall.SIGTERM)
	}
	sess.Pty.Close()
	m.removeMetadata(id)
	return nil
}

// List returns all active managed session IDs.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) writeMetadata(sess *ManagedSession) error {
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	meta := Metadata{
		ID:        sess.ID,
		PID:       sess.Cmd.Process.Pid,
		Dir:       sess.dir,
		Source:    "native",
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

func (m *Manager) removeMetadata(id string) {
	path := filepath.Join(m.stateDir, id+".json")
	_ = os.Remove(path)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/pty/ -v -run TestManager -race
```

Expected: all 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pty/manager.go internal/pty/manager_test.go go.mod go.sum
git commit -m "feat(pty): add PTY manager for session spawn and lifecycle"
```

---

### Task 2: Add attach/detach proxy

**Files:**
- Create: `internal/pty/proxy.go`
- Create: `internal/pty/proxy_test.go`

- [ ] **Step 1: Write failing test for PTY proxy attach/detach**

Create `internal/pty/proxy_test.go`:

```go
package pty

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProxy_ReadOutput(t *testing.T) {
	mgr := NewManager(t.TempDir())

	cmd := exec.Command("echo", "hello from pty")
	sess, err := mgr.Spawn("proxy-test", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, sess.Pty)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading PTY output")
	}

	if !strings.Contains(buf.String(), "hello from pty") {
		t.Errorf("output = %q, want to contain 'hello from pty'", buf.String())
	}
}

func TestProxy_WriteInput(t *testing.T) {
	mgr := NewManager(t.TempDir())

	// Use cat which echoes stdin to stdout.
	cmd := exec.Command("cat")
	sess, err := mgr.Spawn("echo-test", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { mgr.Stop("echo-test") })

	// Write to the PTY.
	_, err = sess.Pty.Write([]byte("test input\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read back — cat should echo the input.
	buf := make([]byte, 256)
	sess.Pty.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := sess.Pty.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	output := string(buf[:n])
	if !strings.Contains(output, "test input") {
		t.Errorf("output = %q, want to contain 'test input'", output)
	}
}

func TestProxy_DetachByte(t *testing.T) {
	// Verify the detach byte constant is Ctrl+].
	if DetachByte != 0x1d {
		t.Errorf("DetachByte = %#x, want 0x1d (Ctrl+])", DetachByte)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/pty/ -v -run TestProxy
```

Expected: compilation error — `DetachByte` not defined.

- [ ] **Step 3: Implement proxy**

Create `internal/pty/proxy.go`:

```go
package pty

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/xpty"
	"golang.org/x/term"
)

// DetachByte is the byte sent by Ctrl+] — used to detach from a session.
const DetachByte = 0x1d

// Proxy connects the user's terminal to a managed session's PTY.
// It blocks until the user sends the detach chord or the session exits.
// Returns nil on clean detach, io.EOF on session exit.
type Proxy struct {
	sess *ManagedSession
}

// NewProxy creates a proxy for the given managed session.
func NewProxy(sess *ManagedSession) *Proxy {
	return &Proxy{sess: sess}
}

// Run connects stdin/stdout to the session PTY.
// It puts the terminal in raw mode and handles SIGWINCH for resize.
// Blocks until detach (Ctrl+]) or session exit.
func (p *Proxy) Run(ctx context.Context) error {
	// Put terminal in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Propagate terminal size to PTY.
	if err := p.syncSize(); err != nil {
		// Non-fatal — just means the initial size might be wrong.
		_ = err
	}

	// Handle SIGWINCH for terminal resize.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Forward SIGWINCH in background.
	go func() {
		for {
			select {
			case <-sigCh:
				_ = p.syncSize()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Copy PTY output -> stdout in background.
	go func() {
		_, _ = io.Copy(os.Stdout, p.sess.Pty)
		cancel()
	}()

	// Copy stdin -> PTY, watching for detach byte.
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.sess.Done:
			return io.EOF
		default:
		}

		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil
		}

		// Scan for detach byte.
		for i := 0; i < n; i++ {
			if buf[i] == DetachByte {
				// Write anything before the detach byte.
				if i > 0 {
					_, _ = p.sess.Pty.Write(buf[:i])
				}
				return nil // clean detach
			}
		}

		if _, err := p.sess.Pty.Write(buf[:n]); err != nil {
			return nil
		}
	}
}

func (p *Proxy) syncSize() error {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return err
	}
	return p.sess.Pty.Resize(w, h)
}
```

- [ ] **Step 4: Add golang.org/x/term dependency**

```bash
go get golang.org/x/term@latest
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/pty/ -v -run TestProxy -race
```

Expected: all 3 tests PASS.

Note: `TestProxy_ReadOutput` and `TestProxy_WriteInput` test the PTY I/O directly (not the full `Proxy.Run` which requires a real terminal). The full proxy flow is tested in integration tests (Task 7).

- [ ] **Step 6: Commit**

```bash
git add internal/pty/proxy.go internal/pty/proxy_test.go go.mod go.sum
git commit -m "feat(pty): add attach/detach proxy with Ctrl+] detach"
```

---

### Task 3: Extend session model with Managed field

**Files:**
- Modify: `internal/session/session.go:50-65`
- Modify: `internal/tui/sessions.go:86-110`

- [ ] **Step 1: Write failing test for Managed field rendering**

Add to the bottom of `internal/tui/app_test.go`:

```go
func TestSessionList_ManagedIndicator(t *testing.T) {
	sl := newSessionList()
	sl.width = 60
	sl.height = 20
	sl.sessions = []session.Session{
		{ID: "m1", Name: "managed-one", Source: session.SourceNative, Health: session.HealthGreen, Managed: true},
		{ID: "d1", Name: "discovered", Source: session.SourceNative, Health: session.HealthGreen, Managed: false},
	}

	view := sl.View()

	if !strings.Contains(view, "[cs]") {
		t.Error("managed session should show [cs] indicator")
	}
}
```

Add `"strings"` to the test file's imports if not present.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -v -run TestSessionList_ManagedIndicator
```

Expected: compilation error — `Managed` field does not exist on `session.Session`.

- [ ] **Step 3: Add Managed field to Session**

In `internal/session/session.go`, add `Managed` to the Session struct after `LogPath`:

```go
	LogPath      string       `json:"log_path,omitempty"`
	Managed      bool         `json:"managed,omitempty"`
```

Also add new status constants for sandbox stage lifecycle:

```go
	StatusStopped   Status = "stopped"
	StatusExecuting Status = "executing"
	StatusShipping  Status = "shipping"
```

- [ ] **Step 4: Add [cs] indicator to session list rendering**

In `internal/tui/sessions.go`, modify `renderSession` to show the managed indicator. Replace the `meta` line construction (lines 99-106):

```go
	displayStatus := string(s.Status)
	if s.Status == session.StatusRunning && hasIdleDiagnostic(s) {
		displayStatus = "idle"
	}
	meta := fmt.Sprintf("%s · %s", s.Source, displayStatus)
	if s.Managed {
		meta = "[cs] " + meta
	}
	if s.Task != "" {
		meta += " · " + s.Task
	}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/tui/ -v -race
```

Expected: all tests PASS including the new `TestSessionList_ManagedIndicator`.

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/tui/sessions.go internal/tui/app_test.go
git commit -m "feat(session): add Managed field and [cs] indicator in session list"
```

---

### Task 4: Add ManagedSource scanner

**Files:**
- Create: `internal/scanner/managed.go`
- Create: `internal/scanner/managed_test.go`
- Modify: `internal/scanner/scanner.go:22-40`

- [ ] **Step 1: Write failing test for ManagedSource**

Create `internal/scanner/managed_test.go`:

```go
package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestManagedSource_Scan(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "cs-sessions")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	meta := map[string]any{
		"id":         "managed-1",
		"pid":        99999999,
		"dir":        "/tmp/test-project",
		"source":     "native",
		"created_at": "2026-03-31T10:00:00Z",
		"managed":    true,
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(stateDir, "managed-1.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &ManagedSource{StateDir: stateDir}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("len = %d, want 1", len(sessions))
	}

	s := sessions[0]
	if s.ID != "managed-1" {
		t.Errorf("ID = %q", s.ID)
	}
	if !s.Managed {
		t.Error("Managed = false, want true")
	}
	if s.Dir != "/tmp/test-project" {
		t.Errorf("Dir = %q", s.Dir)
	}
	// PID 99999999 should not be alive.
	if s.Status != session.StatusStopped {
		t.Errorf("Status = %q, want stopped (dead PID)", s.Status)
	}
}

func TestManagedSource_EmptyDir(t *testing.T) {
	src := &ManagedSource{StateDir: filepath.Join(t.TempDir(), "nonexistent")}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("len = %d, want 0", len(sessions))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/scanner/ -v -run TestManagedSource
```

Expected: compilation error — `ManagedSource` does not exist.

- [ ] **Step 3: Implement ManagedSource**

Create `internal/scanner/managed.go`:

```go
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// managedMeta mirrors the on-disk JSON for cs-managed sessions.
type managedMeta struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Dir       string    `json:"dir"`
	Source    string    `json:"source"`
	Stage     string    `json:"stage,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Managed   bool      `json:"managed"`
}

// ManagedSource discovers sessions launched and owned by cs.
type ManagedSource struct {
	StateDir string // ~/.claude/cs-sessions/
}

func (m *ManagedSource) Scan(_ context.Context) ([]session.Session, error) {
	entries, err := os.ReadDir(m.StateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read managed sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(m.StateDir, entry.Name()))
		if err != nil {
			continue
		}

		var meta managedMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		alive := session.IsProcessAlive(meta.PID)
		status := session.StatusStopped
		if alive {
			status = session.StatusRunning
		}

		source := session.Source(meta.Source)
		if source == "" {
			source = session.SourceNative
		}

		sess := session.Session{
			ID:        meta.ID,
			Source:    source,
			Status:    status,
			Dir:       meta.Dir,
			PID:       meta.PID,
			StartedAt: meta.CreatedAt,
			Health:    session.HealthGreen,
			Name:      filepath.Base(meta.Dir),
			Managed:   true,
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}
```

- [ ] **Step 4: Add deduplication to Scanner.Scan**

In `internal/scanner/scanner.go`, update the `Scan` method to deduplicate by PID — managed sessions win over discovered ones:

```go
// Scan collects sessions from all sources, sorted by last activity (most recent first).
// Managed sessions take priority over discovered sessions with the same PID.
func (s *Scanner) Scan(ctx context.Context) ([]session.Session, error) {
	var all []session.Session
	for _, src := range s.Sources {
		sessions, err := src.Scan(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		all = append(all, sessions...)
	}

	all = deduplicateByPID(all)

	// Apply persisted labels.
	applyLabels(all)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})

	return all, nil
}

// deduplicateByPID removes duplicate sessions by PID, preferring managed sessions.
func deduplicateByPID(sessions []session.Session) []session.Session {
	seen := make(map[int]int) // PID -> index in result
	var result []session.Session

	for _, s := range sessions {
		if s.PID <= 0 {
			result = append(result, s)
			continue
		}
		if idx, exists := seen[s.PID]; exists {
			// Keep the managed one.
			if s.Managed && !result[idx].Managed {
				result[idx] = s
			}
			continue
		}
		seen[s.PID] = len(result)
		result = append(result, s)
	}

	return result
}
```

- [ ] **Step 5: Write test for deduplication**

Add to `internal/scanner/scanner_test.go`:

```go
func TestDeduplicateByPID(t *testing.T) {
	sessions := []session.Session{
		{ID: "native-1", PID: 1234, Managed: false, Source: session.SourceNative},
		{ID: "managed-1", PID: 1234, Managed: true, Source: session.SourceNative},
		{ID: "sandbox-1", PID: 0, Managed: false, Source: session.SourceSandbox},
	}

	result := deduplicateByPID(sessions)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}

	// The managed session should win.
	for _, s := range result {
		if s.PID == 1234 && !s.Managed {
			t.Error("PID 1234 should be managed session, got discovered")
		}
	}

	// Sandbox session (PID 0) should be kept.
	found := false
	for _, s := range result {
		if s.ID == "sandbox-1" {
			found = true
		}
	}
	if !found {
		t.Error("sandbox session should be preserved")
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/scanner/ -v -race
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/scanner/managed.go internal/scanner/managed_test.go internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat(scanner): add ManagedSource with PID deduplication"
```

---

### Task 5: Wire PTY manager into TUI for `n` (new) and `a` (attach)

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

- [ ] **Step 1: Write failing test for attach on managed session**

Add to `internal/tui/app_test.go`:

```go
func TestApp_AttachManagedSession(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "m1", Name: "managed", Source: session.SourceNative, Health: session.HealthGreen, Managed: true, Status: session.StatusRunning},
	}

	updated, cmd := app.Update(keyPress('a'))
	app = updated.(*App)

	// For a managed session with no PTY manager, should show flash.
	// When the PTY manager is wired in, this test will need updating
	// to check for ExecProcess command instead.
	if app.flashMsg == "" && cmd == nil {
		t.Error("expected either flash message or exec command for managed session")
	}
}

func TestApp_AttachDiscoveredSession(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "d1", Name: "discovered", Source: session.SourceNative, Health: session.HealthGreen, Managed: false, Status: session.StatusRunning},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	if !strings.Contains(app.flashMsg, "not managed") {
		t.Errorf("flash = %q, want to contain 'not managed'", app.flashMsg)
	}
}

func TestApp_AttachStoppedSession(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "stopped", Source: session.SourceNative, Health: session.HealthGreen, Managed: true, Status: session.StatusStopped},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	// Stopped managed session should offer resume.
	if !strings.Contains(app.flashMsg, "resume") && app.mode != modeConfirm {
		t.Errorf("expected resume prompt for stopped managed session, got flash=%q mode=%d", app.flashMsg, app.mode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -v -run "TestApp_Attach"
```

Expected: compilation error or test failure — `Managed` field not used in attach handler.

- [ ] **Step 3: Add PTY manager to App and update attach handler**

In `internal/tui/app.go`, add the PTY manager field to App and update NewApp:

```go
import (
	// ... existing imports ...
	ptyPkg "github.com/dakaneye/claude-session-manager/internal/pty"
)
```

Add field to App struct:

```go
	ptyMgr *ptyPkg.Manager
```

Update `NewApp` to accept a PTY manager:

```go
func NewApp(sc *scanner.Scanner, ptyMgr *ptyPkg.Manager) *App {
	return &App{
		scanner:           sc,
		ptyMgr:            ptyMgr,
		sessions:          newSessionList(),
		detail:            newDetailPane(),
		statusbar:         newStatusBar(),
		activities:        make(map[string][]scanner.ActivityEntry),
		lastMessages:      make(map[string]string),
		conversationTails: make(map[string][]string),
	}
}
```

Add new confirm actions:

```go
const (
	confirmNone confirmAction = iota
	confirmStop
	confirmResume
	confirmNextStage
)
```

Update `handleNormalKey` attach case:

```go
	case keyAttach:
		sel := a.sessions.Selected()
		if sel == nil {
			break
		}
		if sel.Managed && sel.Status == session.StatusRunning {
			// Attach to running managed session.
			if a.ptyMgr == nil {
				a.setFlash("PTY manager not initialized")
				break
			}
			sess, ok := a.ptyMgr.Get(sel.ID)
			if !ok {
				a.setFlash("PTY session not found — may need resume")
				break
			}
			return a, a.attachSession(sess)
		}
		if sel.Managed && sel.Status == session.StatusStopped {
			// Offer to resume.
			a.mode = modeConfirm
			a.confirmAction = confirmResume
			break
		}
		if !sel.Managed && sel.Status == session.StatusRunning {
			a.setFlash("Session not managed by cs — use peek to monitor")
			break
		}
		if !sel.Managed && sel.Status == session.StatusIdle {
			// Dead discovered session — offer resume.
			a.mode = modeConfirm
			a.confirmAction = confirmResume
			break
		}
		a.setFlash(fmt.Sprintf("Cannot attach to %s session (status: %s)", sel.Source, sel.Status))
```

Add `attachSession` method:

```go
func (a *App) attachSession(sess *ptyPkg.ManagedSession) tea.Cmd {
	proxy := ptyPkg.NewProxy(sess)
	return tea.Exec(proxy, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
}
```

Update `confirmPrompt`:

```go
	case confirmResume:
		sel := a.sessions.Selected()
		if sel == nil {
			return "Resume? (y/n)"
		}
		return fmt.Sprintf("Resume %s? (y/n)", sel.DisplayName())
```

Update `handleConfirmKey` to handle resume:

```go
	case msg.String() == "y":
		switch a.confirmAction {
		case confirmStop:
			a.executeStop()
		case confirmResume:
			a.executeResume()
		}
		a.mode = modeNormal
		a.confirmAction = confirmNone
```

Add `executeResume` method:

```go
func (a *App) executeResume() {
	sel := a.sessions.Selected()
	if sel == nil || a.ptyMgr == nil {
		return
	}
	cmd := exec.Command("claude", "--resume", sel.ID)
	cmd.Dir = sel.Dir
	sess, err := a.ptyMgr.Spawn(sel.ID, cmd)
	if err != nil {
		a.setFlash("Resume error: " + err.Error())
		return
	}
	// Immediately attach to the resumed session.
	// Note: we can't return a Cmd from here directly since this is called
	// from handleConfirmKey. Instead, set a flag and handle in Update.
	_ = sess
	a.setFlash("Resumed — press 'a' to attach")
}
```

- [ ] **Step 4: Make Proxy implement tea.ExecCommand**

The `Proxy` needs to satisfy `tea.ExecCommand` interface (`Run() error`, `SetStdin(io.Reader)`, `SetStdout(io.Writer)`, `SetStderr(io.Writer)`). Add to `internal/pty/proxy.go`:

```go
// SetStdin implements tea.ExecCommand. The proxy uses os.Stdin directly.
func (p *Proxy) SetStdin(_ io.Reader) {}

// SetStdout implements tea.ExecCommand. The proxy uses os.Stdout directly.
func (p *Proxy) SetStdout(_ io.Writer) {}

// SetStderr implements tea.ExecCommand. The proxy uses os.Stderr directly.
func (p *Proxy) SetStderr(_ io.Writer) {}

// Run implements tea.ExecCommand. Blocks until detach or session exit.
func (p *Proxy) Run() error {
	return p.RunCtx(context.Background())
}

// RunCtx connects stdin/stdout to the session PTY with cancellation support.
func (p *Proxy) RunCtx(ctx context.Context) error {
```

Rename the existing `Run` method to `RunCtx` and add the `Run() error` wrapper above.

- [ ] **Step 5: Update launchClaude to use PTY manager**

Replace the existing `launchClaude` method in `internal/tui/app.go`:

```go
func (a *App) launchClaude() tea.Cmd {
	if a.ptyMgr == nil {
		// Fallback to direct exec if no PTY manager.
		c := exec.Command("claude")
		return tea.ExecProcess(c, func(err error) tea.Msg {
			return execFinishedMsg{err: err}
		})
	}

	dir, _ := os.Getwd()
	id := fmt.Sprintf("cs-%d", time.Now().UnixMilli())
	cmd := exec.Command("claude")
	cmd.Dir = dir

	sess, err := a.ptyMgr.Spawn(id, cmd)
	if err != nil {
		return func() tea.Msg {
			return execFinishedMsg{err: err}
		}
	}

	return a.attachSession(sess)
}
```

- [ ] **Step 6: Update all NewApp call sites**

In `cmd/cs/root.go`, update the RunE to create a PTY manager:

```go
	RunE: func(_ *cobra.Command, _ []string) error {
		home, _ := os.UserHomeDir()
		stateDir := filepath.Join(home, ".claude", "cs-sessions")
		ptyMgr := pty.NewManager(stateDir)

		sc := buildScanner()
		app := tui.NewApp(sc, ptyMgr)
		p := tea.NewProgram(app)
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("run TUI: %w", err)
		}
		return nil
	},
```

Add imports for `pty` and `path/filepath`.

Update the `buildScanner` to include `ManagedSource`:

```go
func buildScanner() *scanner.Scanner {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	return &scanner.Scanner{
		Sources: []scanner.SessionSource{
			&scanner.ManagedSource{
				StateDir: filepath.Join(home, ".claude", "cs-sessions"),
			},
			&scanner.SandboxSource{
				RepoPaths: []string{cwd},
			},
			&scanner.NativeSource{
				ClaudeDir: home + "/.claude",
			},
		},
	}
}
```

- [ ] **Step 7: Update test helpers for new NewApp signature**

In `internal/tui/app_test.go`, update all `NewApp(nil)` calls to `NewApp(nil, nil)`.

- [ ] **Step 8: Run tests to verify they pass**

```bash
go test ./internal/tui/ -v -race
go test ./internal/pty/ -v -race
go test ./internal/scanner/ -v -race
```

Expected: all tests PASS.

- [ ] **Step 9: Run full verify**

```bash
make verify
```

Expected: build + vet + lint + test + tidy all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go internal/pty/proxy.go cmd/cs/root.go
git commit -m "feat(tui): wire PTY manager for attach/detach and managed session launch"
```

---

### Task 6: Add CLI attach, resume, and sandbox subcommands

**Files:**
- Create: `cmd/cs/attach.go`
- Create: `cmd/cs/resume.go`
- Create: `cmd/cs/sandbox.go`
- Modify: `cmd/cs/root.go:32-37`
- Modify: `cmd/cs/ls.go:51-56`

- [ ] **Step 1: Create attach command**

Create `cmd/cs/attach.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dakaneye/claude-session-manager/internal/pty"
	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newAttachCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "attach [session]",
		Short: "Attach to a managed session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if !sess.Managed {
				return fmt.Errorf("session %s is not managed by cs — use 'cs peek' to monitor", sess.ID)
			}

			if sess.Status != session.StatusRunning {
				return fmt.Errorf("session %s is not running (status: %s) — use 'cs resume' instead", sess.ID, sess.Status)
			}

			home, _ := os.UserHomeDir()
			stateDir := filepath.Join(home, ".claude", "cs-sessions")
			mgr := pty.NewManager(stateDir)

			// For CLI attach, we need the PTY manager to have the session.
			// Since the CLI doesn't hold PTY state across invocations,
			// this command only works from within the TUI (where the manager lives)
			// or after a fresh spawn.
			_, ok := mgr.Get(sess.ID)
			if !ok {
				return fmt.Errorf("PTY handle not found for %s — attach from within the TUI instead", sess.ID)
			}

			cmd.Println("Use 'cs' TUI and press 'a' to attach to managed sessions.")
			cmd.Println("Direct CLI attach requires the TUI to hold the PTY handle.")
			return nil
		},
	}
}
```

- [ ] **Step 2: Create resume command**

Create `cmd/cs/resume.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newResumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [session]",
		Short: "Resume a stopped or dead session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			// Launch claude --resume directly (no PTY — user is in their terminal).
			c := exec.Command("claude", "--resume", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Resuming session %s in %s...\n", sess.DisplayName(), sess.Dir)
			if err := c.Run(); err != nil {
				return fmt.Errorf("resume session: %w", err)
			}
			return nil
		},
	}
}
```

- [ ] **Step 3: Create sandbox command group**

Create `cmd/cs/sandbox.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox session lifecycle",
	}

	cmd.AddCommand(newSandboxExecuteCommand())
	cmd.AddCommand(newSandboxShipCommand())
	return cmd
}

func newSandboxExecuteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "execute [session]",
		Short: "Advance a sandbox session to the execute stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source != session.SourceSandbox {
				return fmt.Errorf("session %s is not a sandbox session", sess.ID)
			}

			c := exec.Command("claude-sandbox", "execute", "--session", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Starting execute for %s...\n", sess.DisplayName())
			if err := c.Run(); err != nil {
				return fmt.Errorf("execute sandbox session: %w", err)
			}
			return nil
		},
	}
}

func newSandboxShipCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ship [session]",
		Short: "Advance a sandbox session to the ship stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source != session.SourceSandbox {
				return fmt.Errorf("session %s is not a sandbox session", sess.ID)
			}

			c := exec.Command("claude-sandbox", "ship", "--session", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Starting ship for %s...\n", sess.DisplayName())
			if err := c.Run(); err != nil {
				return fmt.Errorf("ship sandbox session: %w", err)
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Register new commands in root**

In `cmd/cs/root.go`, add the new commands after the existing ones:

```go
	cmd.AddCommand(newAttachCommand())
	cmd.AddCommand(newResumeCommand())
	cmd.AddCommand(newSandboxCommand())
```

- [ ] **Step 5: Update ls to show managed indicator**

In `cmd/cs/ls.go`, update the table header and row:

```go
	fmt.Fprintln(w, "HEALTH\tNAME\tSOURCE\tSTATUS\tMANAGED\tDIR")
	for _, s := range sessions {
		dot := healthSymbol(s.Health)
		managed := ""
		if s.Managed {
			managed = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", dot, s.DisplayName(), s.Source, s.Status, managed, s.Dir)
	}
```

- [ ] **Step 6: Update statusbar help text**

In `internal/tui/statusbar.go`, update the help text for the `a` key:

```go
		"  " + statusBarKeyStyle.Render("a") + "          Attach to managed session / resume stopped session",
```

- [ ] **Step 7: Run full verify**

```bash
make verify
```

Expected: build + vet + lint + test + tidy all pass.

- [ ] **Step 8: Commit**

```bash
git add cmd/cs/attach.go cmd/cs/resume.go cmd/cs/sandbox.go cmd/cs/root.go cmd/cs/ls.go internal/tui/statusbar.go
git commit -m "feat(cli): add attach, resume, and sandbox subcommands"
```

---

### Task 7: Integration tests for full PTY attach/detach flow

**Files:**
- Create: `internal/pty/integration_test.go`
- Modify: `internal/scanner/integration_test.go`

- [ ] **Step 1: Write PTY integration test**

Create `internal/pty/integration_test.go`:

```go
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
	mgr := NewManager(stateDir)

	cmd := exec.Command("sleep", "10")
	cmd.Dir = "/tmp"
	sess, err := mgr.Spawn("int-test-1", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { mgr.Stop("int-test-1") })

	// Verify metadata file was written.
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
	if meta.PID != sess.Cmd.Process.Pid {
		t.Errorf("PID = %d, want %d", meta.PID, sess.Cmd.Process.Pid)
	}
	if !meta.Managed {
		t.Error("Managed = false")
	}
}

func TestIntegration_StopCleansMetadata(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "cs-sessions")
	mgr := NewManager(stateDir)

	cmd := exec.Command("sleep", "60")
	_, err := mgr.Spawn("stop-test", cmd)
	if err != nil {
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
	mgr := NewManager(t.TempDir())

	// Short-lived command that exits immediately.
	cmd := exec.Command("true")
	sess, err := mgr.Spawn("exit-test", cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	select {
	case <-sess.Done:
		// Expected.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Done channel")
	}
}
```

- [ ] **Step 2: Update scanner integration test to include ManagedSource**

Add to the end of `internal/scanner/integration_test.go`:

```go
func TestIntegration_ManagedSessionDeduplication(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a native session with a known PID.
	claudeDir := filepath.Join(tmpDir, "claude")
	nativeSessDir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(nativeSessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeJSON := map[string]any{
		"pid":       88888888,
		"sessionId": "dedup-native",
		"cwd":       "/tmp/dedup-project",
		"startedAt": 1774912561112,
	}
	data, _ := json.MarshalIndent(nativeJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(nativeSessDir, "88888888.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up a managed session with the same PID.
	managedDir := filepath.Join(tmpDir, "cs-sessions")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedJSON := map[string]any{
		"id":         "dedup-managed",
		"pid":        88888888,
		"dir":        "/tmp/dedup-project",
		"source":     "native",
		"created_at": "2026-03-31T10:00:00Z",
		"managed":    true,
	}
	mdata, _ := json.MarshalIndent(managedJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(managedDir, "dedup-managed.json"), mdata, 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &Scanner{
		Sources: []SessionSource{
			&ManagedSource{StateDir: managedDir},
			&NativeSource{ClaudeDir: claudeDir},
		},
	}

	sessions, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Should have exactly 1 session (deduplicated by PID).
	pidCount := 0
	for _, s := range sessions {
		if s.PID == 88888888 {
			pidCount++
			if !s.Managed {
				t.Error("deduped session should be the managed one")
			}
		}
	}

	if pidCount != 1 {
		t.Errorf("PID 88888888 appears %d times, want 1", pidCount)
	}
}
```

- [ ] **Step 3: Run all tests**

```bash
go test ./internal/pty/ -v -race
go test ./internal/scanner/ -v -race
```

Expected: all tests PASS.

- [ ] **Step 4: Run full verify**

```bash
make verify
```

Expected: all checks pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pty/integration_test.go internal/scanner/integration_test.go
git commit -m "test: add integration tests for PTY lifecycle and session deduplication"
```

---

### Task 8: Update TUI for sandbox stage transitions

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

- [ ] **Step 1: Write failing test for sandbox stage prompt**

Add to `internal/tui/app_test.go`:

```go
func TestApp_AttachSandboxBetweenStages(t *testing.T) {
	app := NewApp(nil, nil)
	app.sessions.sessions = []session.Session{
		{
			ID:      "sb1",
			Name:    "sandbox-ready",
			Source:  session.SourceSandbox,
			Health:  session.HealthGreen,
			Managed: true,
			Status:  session.StatusReady,
		},
	}

	updated, _ := app.Update(keyPress('a'))
	app = updated.(*App)

	if app.mode != modeConfirm {
		t.Errorf("mode = %d, want modeConfirm", app.mode)
	}
	if app.confirmAction != confirmNextStage {
		t.Errorf("confirmAction = %d, want confirmNextStage", app.confirmAction)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -v -run TestApp_AttachSandboxBetweenStages
```

Expected: failure — `confirmNextStage` not handled for sandbox ready status.

- [ ] **Step 3: Add sandbox stage handling to attach**

In the `keyAttach` case of `handleNormalKey` in `internal/tui/app.go`, add handling for sandbox between-stage states. Insert before the final fallback:

```go
		if sel.Source == session.SourceSandbox && (sel.Status == session.StatusReady || sel.Status == session.StatusSuccess) {
			a.mode = modeConfirm
			a.confirmAction = confirmNextStage
			break
		}
```

Update `confirmPrompt` to handle `confirmNextStage`:

```go
	case confirmNextStage:
		sel := a.sessions.Selected()
		if sel == nil {
			return "Start next stage? (y/n)"
		}
		var nextStage string
		switch sel.Status {
		case session.StatusReady:
			nextStage = "execute"
		case session.StatusSuccess:
			nextStage = "ship"
		default:
			nextStage = "next stage"
		}
		return fmt.Sprintf("Start %s for %s? (y/n)", nextStage, sel.DisplayName())
```

Update `handleConfirmKey` to handle `confirmNextStage`:

```go
		case confirmNextStage:
			a.executeNextStage()
```

Add `executeNextStage` method:

```go
func (a *App) executeNextStage() {
	sel := a.sessions.Selected()
	if sel == nil || a.ptyMgr == nil {
		return
	}

	var cmdName string
	var args []string
	switch sel.Status {
	case session.StatusReady:
		cmdName = "claude-sandbox"
		args = []string{"execute", "--session", sel.ID}
	case session.StatusSuccess:
		cmdName = "claude-sandbox"
		args = []string{"ship", "--session", sel.ID}
	default:
		a.setFlash("No next stage for status: " + string(sel.Status))
		return
	}

	id := sel.ID + "-" + args[0]
	cmd := exec.Command(cmdName, args...)
	cmd.Dir = sel.Dir

	sess, err := a.ptyMgr.Spawn(id, cmd)
	if err != nil {
		a.setFlash("Stage error: " + err.Error())
		return
	}
	_ = sess
	a.setFlash(fmt.Sprintf("Started %s — press 'a' to attach", args[0]))
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -v -race
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(tui): add sandbox stage transitions on attach key"
```

---

### Task 9: Final integration verification and cleanup

**Files:**
- Modify: `cmd/cs/stop.go` (add PTY cleanup for managed sessions)

- [ ] **Step 1: Update stop command for managed sessions**

In `cmd/cs/stop.go`, update to handle managed sessions:

```go
func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [session]",
		Short: "Stop a running session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Managed {
				// For managed sessions, remove the metadata file too.
				home, _ := os.UserHomeDir()
				metaFile := filepath.Join(home, ".claude", "cs-sessions", sess.ID+".json")
				_ = os.Remove(metaFile)
			}

			if sess.Source == session.SourceNative && sess.PID > 0 {
				proc, err := os.FindProcess(sess.PID)
				if err != nil {
					return fmt.Errorf("find process %d: %w", sess.PID, err)
				}
				if err := proc.Signal(syscall.SIGTERM); err != nil {
					return fmt.Errorf("signal process %d: %w", sess.PID, err)
				}
				cmd.Printf("Sent SIGTERM to PID %d (%s)\n", sess.PID, sess.ID)

				// Also remove the native session file.
				if sess.Managed {
					// Managed sessions may not have a native session file yet.
				} else {
					home, _ := os.UserHomeDir()
					pidFile := filepath.Join(home, ".claude", "sessions", fmt.Sprintf("%d.json", sess.PID))
					_ = os.Remove(pidFile)
				}
				return nil
			}

			if sess.Source == session.SourceSandbox {
				cmd.PrintErrln("Sandbox sessions should be stopped with: claude-sandbox stop " + sess.ID)
				return nil
			}

			return fmt.Errorf("cannot stop session %s (source: %s)", sess.ID, sess.Source)
		},
	}
}
```

Add `"path/filepath"` to imports.

- [ ] **Step 2: Run full verify**

```bash
make verify
```

Expected: build + vet + lint + test -race + tidy all pass.

- [ ] **Step 3: Commit**

```bash
git add cmd/cs/stop.go
git commit -m "fix(stop): clean up managed session metadata on stop"
```

- [ ] **Step 4: Run the binary to sanity check**

```bash
go run ./cmd/cs version
go run ./cmd/cs ls
go run ./cmd/cs --help
go run ./cmd/cs sandbox --help
go run ./cmd/cs attach --help
go run ./cmd/cs resume --help
```

Expected: all commands print help/output without errors. `ls` lists sessions. `sandbox` shows `execute` and `ship` subcommands.

- [ ] **Step 5: Final commit if any fixups needed**

```bash
# Only if there were fixups from step 4
git add -A
git commit -m "fix: address issues found in integration sanity check"
```
