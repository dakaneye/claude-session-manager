package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/muesli/cancelreader"
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

// ansiExitAltScreen exits the alternate screen buffer. The PTY'd process
// (e.g. claude) typically enters its own alt-screen when attached; if it
// is still running when we detach, the terminal stays in that alt-screen
// and bubbletea's re-enter becomes a no-op, leaving the parent TUI layered
// on top of the leftover content. Writing this sequence before handing
// control back forces us onto the main screen so bubbletea's alt-screen
// re-entry opens a clean buffer.
const ansiExitAltScreen = "\x1b[?1049l"

// ansiSetTitle and ansiResetTitle manage the terminal window title.
// Setting it during attach gives the user a persistent reminder of
// the detach chord even while claude takes over the screen.
const (
	ansiSetTitle   = "\x1b]2;cs attached — Ctrl+] to detach\x07"
	ansiResetTitle = "\x1b]2;cs\x07"
)

// attachHint is printed briefly before claude's alt-screen covers it
// so the user sees the detach chord at least once per attach.
const attachHint = "\r\n[cs] Attached — press Ctrl+] to detach\r\n"

// Run blocks until the user detaches (Ctrl+]) or the session exits.
// It puts the terminal in raw mode, syncs terminal size to the PTY,
// and copies I/O bidirectionally.
//
// Cleanup ordering is critical: on return we must (1) cancel the I/O
// readers to unblock their goroutines, (2) wait for those goroutines to
// actually exit, and (3) close the readers so their kqueue/epoll fds are
// released. If we skip step 3, bubbletea's own cancelreader created on
// os.Stdin after tea.Exec returns will fight a zombie registration and
// input stops reaching the parent TUI.
func (p *Proxy) Run() error {
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("make terminal raw: %w", err)
	}

	stdinReader, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		_ = term.Restore(fd, oldState)
		return fmt.Errorf("create cancelable stdin reader: %w", err)
	}

	ptyReader, err := cancelreader.NewReader(p.sess.Pty)
	if err != nil {
		_ = stdinReader.Close()
		_ = term.Restore(fd, oldState)
		return fmt.Errorf("create cancelable pty reader: %w", err)
	}

	if err := p.syncTermSize(fd); err != nil {
		_ = ptyReader.Close()
		_ = stdinReader.Close()
		_ = term.Restore(fd, oldState)
		return fmt.Errorf("sync terminal size: %w", err)
	}

	// Show the detach chord briefly (main screen + window title) before
	// claude's alt-screen obscures the inline hint. The title persists
	// for the entire attached session.
	_, _ = os.Stdout.Write([]byte(attachHint))
	_, _ = os.Stdout.Write([]byte(ansiSetTitle))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	var sigWG sync.WaitGroup
	sigWG.Add(1)
	go func() {
		defer sigWG.Done()
		for range sigCh {
			_ = p.syncTermSize(fd)
		}
	}()

	var ioWG sync.WaitGroup
	ioWG.Add(2)

	ptyDone := make(chan error, 1)
	go func() {
		defer ioWG.Done()
		_, err := io.Copy(os.Stdout, ptyReader)
		ptyDone <- err
	}()

	stdinDone := make(chan error, 1)
	go func() {
		defer ioWG.Done()
		buf := make([]byte, 256)
		for {
			n, err := stdinReader.Read(buf)
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

	defer func() {
		// Cancel readers so blocked I/O goroutines unblock...
		stdinReader.Cancel()
		ptyReader.Cancel()
		// ...then wait for them to actually exit before closing.
		ioWG.Wait()
		// Close readers to release kqueue/epoll fds registered on the
		// underlying file descriptors.
		_ = stdinReader.Close()
		_ = ptyReader.Close()
		// Stop the SIGWINCH handler and let its goroutine drain.
		signal.Stop(sigCh)
		close(sigCh)
		sigWG.Wait()
		// Exit any alt-screen the PTY'd process left us in, reset the
		// window title, then restore the terminal mode. Order matters:
		// write escapes while still in raw mode so they aren't line-buffered.
		_, _ = os.Stdout.Write([]byte(ansiExitAltScreen))
		_, _ = os.Stdout.Write([]byte(ansiResetTitle))
		_ = term.Restore(fd, oldState)
	}()

	select {
	case <-p.sess.Done:
		return io.EOF
	case err := <-ptyDone:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, cancelreader.ErrCanceled) {
			return fmt.Errorf("pty read: %w", err)
		}
		return nil
	case err := <-stdinDone:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, cancelreader.ErrCanceled) {
			return fmt.Errorf("stdin read: %w", err)
		}
		// Detach, stdin closed, or canceled.
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
