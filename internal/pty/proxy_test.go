package pty

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestDetachByte(t *testing.T) {
	if DetachByte != 0x1d {
		t.Errorf("DetachByte = %#x, want 0x1d", DetachByte)
	}
}

func TestDetachByteDetection(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantStop bool
	}{
		{"no detach byte", []byte("hello world"), false},
		{"detach byte alone", []byte{DetachByte}, true},
		{"detach byte in middle", []byte{'a', DetachByte, 'b'}, true},
		{"detach byte at end", []byte{'a', 'b', DetachByte}, true},
		{"empty input", []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for i := range len(tt.input) {
				if tt.input[i] == DetachByte {
					found = true
					break
				}
			}
			if found != tt.wantStop {
				t.Errorf("detach found = %v, want %v", found, tt.wantStop)
			}
		})
	}
}

func TestProxy_SessionDoneClosesProxy(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "exit-quickly")
	if err := m.Spawn(t.Context(), "done-test", cmd, "/tmp", session.SourceNative); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), "done-test") })

	sess, ok := m.Get("done-test")
	if !ok {
		t.Fatal("session not found")
	}

	select {
	case <-sess.Done:
		// Process exited, which would cause Proxy.Run() to return io.EOF
		// via the <-p.sess.Done select case.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Done channel")
	}
}

func TestNewProxy(t *testing.T) {
	sess := &ManagedSession{ID: "test"}
	p := NewProxy(sess)
	if p.sess != sess {
		t.Error("NewProxy did not store session reference")
	}
}

func TestProxy_SettersAreNoOps(t *testing.T) {
	p := NewProxy(&ManagedSession{})
	// These should not panic.
	p.SetStdin(nil)
	p.SetStdout(nil)
	p.SetStderr(nil)
}

func TestProxy_PTYReadOutput(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("echo", "proxy-read-test")
	if err := m.Spawn(t.Context(), "pty-read", cmd, "/tmp", session.SourceNative); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), "pty-read") })

	sess, ok := m.Get("pty-read")
	if !ok {
		t.Fatal("session not found")
	}

	output := readPTYUntil(t, sess, "proxy-read-test", 5*time.Second)
	if !strings.Contains(output, "proxy-read-test") {
		t.Errorf("PTY output = %q, want to contain %q", output, "proxy-read-test")
	}
}

func TestProxy_PTYWriteAndReadBack(t *testing.T) {
	stateDir := t.TempDir()
	m := NewManager(stateDir)

	cmd := exec.Command("cat")
	if err := m.Spawn(t.Context(), "pty-write", cmd, "/tmp", session.SourceNative); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), "pty-write") })

	sess, ok := m.Get("pty-write")
	if !ok {
		t.Fatal("session not found")
	}

	// Write to the PTY (cat will echo it back via the terminal driver).
	input := "hello-from-proxy\n"
	if _, err := sess.Pty.Write([]byte(input)); err != nil {
		t.Fatalf("PTY write: %v", err)
	}

	output := readPTYUntil(t, sess, "hello-from-proxy", 5*time.Second)
	if !strings.Contains(output, "hello-from-proxy") {
		t.Errorf("echo output = %q, want to contain %q", output, "hello-from-proxy")
	}
}

// readPTYUntil reads from the session's PTY until the output contains want
// or the timeout expires. It reads in a goroutine to avoid blocking the test
// on a PTY read that may never return.
func readPTYUntil(t *testing.T, sess *ManagedSession, want string, timeout time.Duration) string {
	t.Helper()

	type readResult struct {
		data []byte
		err  error
	}

	results := make(chan readResult, 64)
	done := make(chan struct{})
	defer close(done)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := sess.Pty.Read(buf)
			select {
			case <-done:
				return
			case results <- readResult{data: append([]byte(nil), buf[:n]...), err: err}:
			}
			if err != nil {
				return
			}
		}
	}()

	var output strings.Builder
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out reading PTY; got so far: %q", output.String())
		case r := <-results:
			if len(r.data) > 0 {
				output.Write(r.data)
			}
			if strings.Contains(output.String(), want) {
				return output.String()
			}
			if r.err != nil {
				// EIO is expected after process exits on some platforms.
				return output.String()
			}
		}
	}
}
