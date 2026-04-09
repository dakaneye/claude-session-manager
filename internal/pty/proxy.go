package pty

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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

// kittyEscapeForDetachByte returns the Kitty keyboard protocol byte
// sequence corresponding to a control byte (0x01..0x1f). For Ctrl+]
// (0x1d) this is "\x1b[93;5u" because Kitty encodes a key event as
// CSI <unicode>;<modifier> u where unicode is the base printable form
// of the key (']' = 93) and modifier 5 means Ctrl. Returns nil for
// bytes that aren't standard control codes.
//
// We need this because bubbletea v2 enables the Kitty keyboard protocol
// on the terminal and doesn't disable it before tea.Exec hands control
// to our proxy, so terminals that support the protocol (e.g. Warp) send
// the multi-byte encoded form instead of the legacy single byte.
func kittyEscapeForDetachByte(b byte) []byte {
	var codepoint int
	switch {
	case b >= 0x01 && b <= 0x1a:
		// Ctrl+a..Ctrl+z → 'a'..'z' (97..122).
		codepoint = int(b) + 0x60
	case b >= 0x1b && b <= 0x1f:
		// Ctrl+[ \ ] ^ _ → '[' '\' ']' '^' '_' (91..95).
		codepoint = int(b) + 0x40
	default:
		return nil
	}
	return fmt.Appendf(nil, "\x1b[%d;5u", codepoint)
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

// Run blocks until the user detaches (the configured detach chord) or
// the session exits. It puts the terminal in raw mode, syncs terminal
// size to the PTY, and copies I/O bidirectionally.
//
// Stdin is read directly with os.Stdin.Read (no cancelreader). An earlier
// version wrapped stdin in cancelreader to avoid leaking the read goroutine
// after detach, but cancelreader's kqueue path on macOS doesn't reliably
// wake on tty input — it broke detach detection in Warp. The PTY reader
// (whose fd is a pty master, not a tty) keeps cancelreader so we can
// unblock io.Copy on detach without waiting for claude to write more bytes.
//
// The stdin goroutine is allowed to leak when the session ends via
// p.sess.Done — Manager.Spawn's cleanup goroutine closes the PTY before
// closing Done, so the leaked goroutine's next forward to the PTY fails
// and it exits. Cost: at most one keystroke gets eaten after a session
// exits while the user is back in the TUI.
func (p *Proxy) Run() error {
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("make terminal raw: %w", err)
	}

	ptyReader, err := cancelreader.NewReader(p.sess.Pty)
	if err != nil {
		_ = term.Restore(fd, oldState)
		return fmt.Errorf("create cancelable pty reader: %w", err)
	}

	if err := p.syncTermSize(fd); err != nil {
		_ = ptyReader.Close()
		_ = term.Restore(fd, oldState)
		return fmt.Errorf("sync terminal size: %w", err)
	}

	detachByte, detachName := detachByteFromEnv()
	kittyDetach := kittyEscapeForDetachByte(detachByte)

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

	// PTY → stdout. We track this goroutine in a WaitGroup so the defer
	// can wait for it after canceling the PTY reader.
	var ptyWG sync.WaitGroup
	ptyWG.Add(1)
	ptyDone := make(chan error, 1)
	go func() {
		defer ptyWG.Done()
		_, err := io.Copy(os.Stdout, ptyReader)
		ptyDone <- err
	}()

	// Optional file-based debug log of every stdin read. Enable with
	// CS_PROXY_DEBUG=/tmp/cs-proxy.log (or any writable path). Useful for
	// answering "did the detach chord byte actually reach the proxy".
	dbg := openProxyDebugLog()
	defer dbg.close()

	// Stdin → PTY, watching for the detach chord. Direct os.Stdin.Read;
	// see Run godoc for why this leaks on session exit by design.
	//
	// We match BOTH the legacy single-byte form (0x1d for Ctrl+]) and the
	// Kitty keyboard protocol multi-byte form (\x1b[93;5u for Ctrl+])
	// because bubbletea v2 enables Kitty keyboard on startup and doesn't
	// disable it on releaseTerminal, so when our proxy reads stdin a
	// Kitty-aware terminal (e.g. Warp) sends the encoded form.
	stdinDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			dbg.logRead(n, err, buf)
			if err != nil {
				stdinDone <- err
				return
			}
			detached := false
			for i := range n {
				if buf[i] == detachByte {
					dbg.log("detach byte matched (legacy 0x%02x), exiting stdin loop", detachByte)
					detached = true
					break
				}
			}
			if !detached && len(kittyDetach) > 0 && bytes.Contains(buf[:n], kittyDetach) {
				dbg.log("detach byte matched (kitty %q), exiting stdin loop", string(kittyDetach))
				detached = true
			}
			if detached {
				stdinDone <- nil
				return
			}
			if _, err := p.sess.Pty.Write(buf[:n]); err != nil {
				stdinDone <- err
				return
			}
		}
	}()

	defer func() {
		// Unblock the PTY reader (the stdin goroutine either already
		// exited via the detach byte path or is allowed to leak — see
		// godoc) and wait for it.
		ptyReader.Cancel()
		ptyWG.Wait()
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

// proxyDebug is an opt-in file-based logger for the stdin read loop.
// It exists so we can answer "did the detach chord byte actually reach
// the proxy?" without polluting the attached terminal with stderr writes.
type proxyDebug struct {
	f *os.File
}

func openProxyDebugLog() *proxyDebug {
	path := os.Getenv("CS_PROXY_DEBUG")
	if path == "" {
		return &proxyDebug{}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return &proxyDebug{}
	}
	d := &proxyDebug{f: f}
	d.log("proxy debug log opened (pid=%d)", os.Getpid())
	return d
}

func (d *proxyDebug) close() {
	if d.f == nil {
		return
	}
	d.log("proxy debug log closing")
	_ = d.f.Close()
}

func (d *proxyDebug) log(format string, args ...any) {
	if d.f == nil {
		return
	}
	_, _ = fmt.Fprintf(d.f, "%s "+format+"\n",
		append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

func (d *proxyDebug) logRead(n int, err error, buf []byte) {
	if d.f == nil {
		return
	}
	hex := make([]string, 0, n)
	for i := 0; i < n && i < 16; i++ {
		hex = append(hex, fmt.Sprintf("%02x", buf[i]))
	}
	suffix := ""
	if n > 16 {
		suffix = fmt.Sprintf(" ...(+%d)", n-16)
	}
	d.log("stdin read: n=%d err=%v bytes=[%s]%s", n, err, strings.Join(hex, " "), suffix)
}
