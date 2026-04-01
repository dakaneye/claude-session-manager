package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// DetachByte is the byte sent when the user presses Ctrl+] to detach.
const DetachByte = 0x1d

// Proxy connects the user's terminal to a managed session's PTY for
// bidirectional I/O. It implements the tea.ExecCommand interface.
type Proxy struct {
	sess *ManagedSession
}

// NewProxy creates a Proxy that bridges stdin/stdout to sess's PTY.
func NewProxy(sess *ManagedSession) *Proxy {
	return &Proxy{sess: sess}
}

// SetStdin is a no-op; Proxy reads from os.Stdin directly.
func (p *Proxy) SetStdin(io.Reader) {}

// SetStdout is a no-op; Proxy writes to os.Stdout directly.
func (p *Proxy) SetStdout(io.Writer) {}

// SetStderr is a no-op; Proxy writes to os.Stderr directly.
func (p *Proxy) SetStderr(io.Writer) {}

// Run blocks until the user detaches (Ctrl+]) or the session exits.
// It puts the terminal in raw mode, syncs terminal size to the PTY,
// and copies I/O bidirectionally.
func (p *Proxy) Run() error {
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("make terminal raw: %w", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	if err := p.syncTermSize(fd); err != nil {
		return fmt.Errorf("sync terminal size: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	go func() {
		for range sigCh {
			_ = p.syncTermSize(fd)
		}
	}()

	ptyDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(os.Stdout, p.sess.Pty)
		ptyDone <- err
	}()

	stdinDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				stdinDone <- err
				return
			}
			for i := range n {
				if buf[i] == DetachByte {
					stdinDone <- nil
					return
				}
			}
			if _, err := p.sess.Pty.Write(buf[:n]); err != nil {
				stdinDone <- err
				return
			}
		}
	}()

	select {
	case <-p.sess.Done:
		return io.EOF
	case err := <-ptyDone:
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("pty read: %w", err)
		}
		return nil
	case err := <-stdinDone:
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("stdin read: %w", err)
		}
		// Detach or stdin closed.
		return nil
	}
}

func (p *Proxy) syncTermSize(fd int) error {
	w, h, err := term.GetSize(fd)
	if err != nil {
		return fmt.Errorf("get terminal size: %w", err)
	}
	if err := p.sess.Pty.Resize(w, h); err != nil {
		return fmt.Errorf("resize pty: %w", err)
	}
	return nil
}
