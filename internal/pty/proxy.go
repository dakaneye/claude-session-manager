package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// DefaultDetachByte is the byte sent when the user presses Ctrl+] to detach.
// Ctrl+] is intercepted by some terminal emulators (notably Warp) before it
// reaches the running process, so this can be overridden via CS_DETACH_BYTE.
const DefaultDetachByte = 0x1d

// DetachByte is the byte we watch for in the attached session's stdin to
// trigger a detach. Retained for backwards compatibility; prefer
// detachByteFromEnv which honors the CS_DETACH_BYTE override.
const DetachByte = DefaultDetachByte

// detachByteFromEnv resolves the detach chord byte from the CS_DETACH_BYTE
// environment variable, falling back to DefaultDetachByte. Accepted forms:
//
//	ctrl-q, ^Q, 0x11, 17
//
// Case-insensitive. Returns (byte, name) where name is a human-readable
// chord label for the inline attach hint.
func detachByteFromEnv() (byte, string) {
	raw := strings.TrimSpace(os.Getenv("CS_DETACH_BYTE"))
	if raw == "" {
		return DefaultDetachByte, "Ctrl+]"
	}
	lower := strings.ToLower(raw)

	// ctrl-X or ctrl+X
	for _, prefix := range []string{"ctrl-", "ctrl+"} {
		if strings.HasPrefix(lower, prefix) {
			rest := lower[len(prefix):]
			if len(rest) == 1 {
				b, name := ctrlByte(rest[0])
				if b != 0 {
					return b, name
				}
			}
		}
	}
	// ^X notation
	if len(raw) == 2 && raw[0] == '^' {
		b, name := ctrlByte(raw[1])
		if b != 0 {
			return b, name
		}
	}
	// 0x1d / 0X1D
	if v, err := strconv.ParseUint(raw, 0, 8); err == nil {
		return byte(v), fmt.Sprintf("byte 0x%02x", byte(v))
	}
	// Plain decimal as a final fallback (0.. matches here too but that's fine).
	if v, err := strconv.ParseUint(raw, 10, 8); err == nil {
		return byte(v), fmt.Sprintf("byte 0x%02x", byte(v))
	}
	// Unparseable — fall back to the default and note it so the hint tells
	// the user what they actually got.
	return DefaultDetachByte, "Ctrl+] (unparseable CS_DETACH_BYTE)"
}

// ctrlByte converts a single letter or punctuation character to its
// Ctrl-combination byte value. Returns (0, "") for characters that don't
// have a meaningful control code.
func ctrlByte(r byte) (byte, string) {
	switch {
	case r >= 'a' && r <= 'z':
		return r - 'a' + 1, fmt.Sprintf("Ctrl+%c", r-'a'+'A')
	case r >= 'A' && r <= 'Z':
		return r - 'A' + 1, fmt.Sprintf("Ctrl+%c", r)
	case r == ']':
		return 0x1d, "Ctrl+]"
	case r == '\\':
		return 0x1c, `Ctrl+\`
	case r == '^':
		return 0x1e, "Ctrl+^"
	case r == '_':
		return 0x1f, "Ctrl+_"
	case r == '[':
		return 0x1b, "Ctrl+["
	}
	return 0, ""
}

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

// ansiResetTitle clears the attach title on detach. The set-title is
// computed per-attach in Run so it can reflect the configured chord,
// but some terminals (e.g. Warp) ignore OSC 2 anyway; we leave it in
// place as a best-effort hint for terminals that honor it.
const ansiResetTitle = "\x1b]2;cs\x07"

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

	detachByte, detachName := detachByteFromEnv()

	// Show the detach chord briefly (main screen + window title) before
	// claude's alt-screen obscures the inline hint. The title persists
	// for the entire attached session in terminals that honor OSC 2.
	hint := fmt.Sprintf("\r\n[cs] Attached — press %s to detach\r\n", detachName)
	_, _ = os.Stdout.Write([]byte(hint))
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]2;cs attached — %s to detach\x07", detachName)

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
				if buf[i] == detachByte {
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
