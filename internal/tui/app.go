package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	ptyPkg "github.com/dakaneye/claude-session-manager/internal/pty"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

const tickInterval = 3 * time.Second

type tickMsg struct {
	sessions          []session.Session
	activities        map[string][]scanner.ActivityEntry
	lastMessages      map[string]string
	conversationTails map[string][]string
	err               error
}

type execFinishedMsg struct {
	err error
}

type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmStop
	confirmResume
	confirmNextStage
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeLabel
	modeConfirm
	modeNewType
	modeNewDir
)

type newSessionType int

const (
	newSessionNative newSessionType = iota
	newSessionSandbox
)

// App is the root Bubbletea model.
type App struct {
	scanner           *scanner.Scanner
	ptyMgr            *ptyPkg.Manager
	sessions          sessionList
	detail            detailPane
	statusbar         statusBar
	width             int
	height            int
	activities        map[string][]scanner.ActivityEntry
	lastMessages      map[string]string
	conversationTails map[string][]string

	// Interactive input state.
	mode           inputMode
	labelInput     string
	confirmAction  confirmAction
	newSessionKind newSessionType
	newSessionDir  string
	flashMsg       string
	flashExpiry    time.Time
}

// NewApp creates a TUI application backed by the given scanner.
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

func (a *App) Init() tea.Cmd {
	return a.tick()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateLayout()
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case tickMsg:
		if msg.err == nil {
			a.sessions.SetSessions(msg.sessions)
			a.activities = msg.activities
			a.lastMessages = msg.lastMessages
			a.conversationTails = msg.conversationTails
			a.updateDetail()
		}
		// Clear expired flash messages.
		if a.flashMsg != "" && time.Now().After(a.flashExpiry) {
			a.flashMsg = ""
		}
		return a, a.tick()

	case execFinishedMsg:
		// Force a full screen clear and redraw after the proxy returns.
		// Bubbletea's differential renderer assumes the alt-screen buffer
		// matches its lastView, but the proxy/PTY interaction can leave
		// stale content in the buffer. ClearScreen forces a clean slate.
		return a, tea.ClearScreen
	}

	return a, nil
}

func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Loading...")
	}

	listWidth := a.width * 30 / 100
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := a.width - listWidth
	contentHeight := a.height - 2

	a.sessions.SetSize(listWidth-2, contentHeight-2)
	a.detail.SetSize(detailWidth-2, contentHeight-2)
	a.statusbar.SetWidth(a.width)

	var body string
	if a.statusbar.showHelp {
		helpPane := paneStyle.
			Width(a.width - 2).
			Height(contentHeight - 2).
			Render(a.statusbar.HelpContent())
		body = paneTitleStyle.Render(" Help ") + "\n" + helpPane
	} else {
		leftContent := a.sessions.View()
		rightContent := a.detail.View()

		sel := a.sessions.Selected()
		leftTitle := " Sessions "
		rightTitle := " Detail "
		if sel != nil {
			name := sel.DisplayName()
			rightTitle = " " + name + " "
			if a.detail.peeking {
				rightTitle = " " + name + " [PEEK] "
			}
		}

		leftPane := paneStyle.
			Width(listWidth - 2).
			Height(contentHeight - 2).
			Render(leftContent)
		leftPane = paneTitleStyle.Render(leftTitle) + "\n" + leftPane

		rightPane := paneStyle.
			Width(detailWidth - 2).
			Height(contentHeight - 2).
			Render(rightContent)
		rightPane = paneTitleStyle.Render(rightTitle) + "\n" + rightPane

		body = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	}

	var statusLine string
	switch a.mode {
	case modeLabel:
		statusLine = a.statusbar.RenderInput("Label: ", a.labelInput)
	case modeConfirm:
		statusLine = a.statusbar.RenderConfirm(a.confirmPrompt())
	case modeNewType:
		statusLine = a.statusbar.RenderConfirm("New session: [n]ative / [s]andbox / [esc] cancel")
	case modeNewDir:
		prompt := "Dir (native): "
		if a.newSessionKind == newSessionSandbox {
			prompt = "Dir (sandbox): "
		}
		statusLine = a.statusbar.RenderInput(prompt, a.newSessionDir)
	default:
		if a.flashMsg != "" && time.Now().Before(a.flashExpiry) {
			statusLine = a.statusbar.RenderFlash(a.flashMsg)
		} else {
			statusLine = a.statusbar.View()
		}
	}

	view := lipgloss.JoinVertical(lipgloss.Left, body, statusLine)

	v := tea.NewView(view)
	v.AltScreen = true
	return v
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch a.mode {
	case modeLabel:
		return a.handleLabelKey(msg)
	case modeConfirm:
		return a.handleConfirmKey(msg)
	case modeNewType:
		return a.handleNewTypeKey(msg)
	case modeNewDir:
		return a.handleNewDirKey(msg)
	default:
		return a.handleNormalKey(msg)
	}
}

func (a *App) handleNormalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	action := parseKey(msg)
	switch action {
	case keyQuit:
		return a, tea.Quit
	case keyUp:
		a.sessions.Up()
		a.updateDetail()
	case keyDown:
		a.sessions.Down()
		a.updateDetail()
	case keyPeek:
		a.detail.TogglePeek()
	case keyHelp:
		a.statusbar.ToggleHelp()
	case keyLabel:
		if a.sessions.Selected() != nil {
			a.mode = modeLabel
			a.labelInput = ""
		}
	case keyStop:
		if sel := a.sessions.Selected(); sel != nil {
			a.mode = modeConfirm
			a.confirmAction = confirmStop
		}
	case keyNew:
		a.mode = modeNewType
		a.newSessionKind = newSessionNative
		a.newSessionDir = ""
	case keyAttach:
		sel := a.sessions.Selected()
		if sel == nil {
			break
		}
		if sel.Managed && sel.Status == session.StatusRunning {
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
			a.mode = modeConfirm
			a.confirmAction = confirmResume
			break
		}
		if !sel.Managed && sel.Status == session.StatusRunning {
			a.setFlash("Session not managed by cs — use peek to monitor")
			break
		}
		if !sel.Managed && sel.Status == session.StatusIdle {
			a.mode = modeConfirm
			a.confirmAction = confirmResume
			break
		}
		if sel.Source == session.SourceSandbox && (sel.Status == session.StatusReady || sel.Status == session.StatusSuccess) {
			a.mode = modeConfirm
			a.confirmAction = confirmNextStage
			break
		}
		a.setFlash(fmt.Sprintf("Cannot attach to %s session (status: %s)", sel.Source, sel.Status))
	}
	return a, nil
}

func isEnter(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEnter || msg.Code == tea.KeyReturn || msg.String() == "enter"
}

func isEscape(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEscape || msg.String() == "esc" || msg.String() == "escape"
}

func isBackspace(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyBackspace || msg.String() == "backspace"
}

func (a *App) handleLabelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscape(msg):
		a.mode = modeNormal
		a.labelInput = ""
	case isEnter(msg):
		if sel := a.sessions.Selected(); sel != nil && a.labelInput != "" {
			if err := session.WriteLabel(sel.ID, a.labelInput); err != nil {
				a.setFlash("Label error: " + err.Error())
			} else {
				a.setFlash("Labeled: " + a.labelInput)
			}
		}
		a.mode = modeNormal
		a.labelInput = ""
	case isBackspace(msg):
		if len(a.labelInput) > 0 {
			a.labelInput = a.labelInput[:len(a.labelInput)-1]
		}
	default:
		if msg.Text != "" {
			a.labelInput += msg.Text
		}
	}
	return a, nil
}

func (a *App) handleNewTypeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscape(msg):
		a.mode = modeNormal
	case msg.String() == "n":
		a.newSessionKind = newSessionNative
		a.enterNewDirMode()
	case msg.String() == "s":
		a.newSessionKind = newSessionSandbox
		a.enterNewDirMode()
	}
	return a, nil
}

func (a *App) enterNewDirMode() {
	a.mode = modeNewDir
	if cwd, err := os.Getwd(); err == nil {
		a.newSessionDir = cwd
	}
}

func (a *App) handleNewDirKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscape(msg):
		a.mode = modeNormal
		a.newSessionDir = ""
	case isEnter(msg):
		dir := a.newSessionDir
		kind := a.newSessionKind
		a.mode = modeNormal
		a.newSessionDir = ""
		if dir == "" {
			a.setFlash("Directory required")
			return a, nil
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			a.setFlash("Invalid directory: " + dir)
			return a, nil
		}
		// Sandbox sessions require a git repo so claude-sandbox can
		// create worktrees. Validate up-front rather than letting the
		// spawned process fail with a cryptic error.
		if kind == newSessionSandbox && !isGitRepo(dir) {
			a.setFlash("Sandbox requires a git repo: " + dir)
			return a, nil
		}
		return a, a.launchNewSession(kind, dir)
	case isBackspace(msg):
		if len(a.newSessionDir) > 0 {
			a.newSessionDir = a.newSessionDir[:len(a.newSessionDir)-1]
		}
	default:
		if msg.Text != "" {
			a.newSessionDir += msg.Text
		}
	}
	return a, nil
}

// isGitRepo reports whether dir (or any ancestor) contains a .git entry.
// Used to validate sandbox session directories before spawning, since
// claude-sandbox spec calls its own findRepoRoot() and errors out otherwise.
func isGitRepo(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

func (a *App) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscape(msg):
		a.mode = modeNormal
		a.confirmAction = confirmNone
	case msg.String() == "y":
		switch a.confirmAction {
		case confirmStop:
			a.executeStop()
		case confirmResume:
			a.executeResume()
		case confirmNextStage:
			a.executeNextStage()
		}
		a.mode = modeNormal
		a.confirmAction = confirmNone
	case msg.String() == "n":
		a.mode = modeNormal
		a.confirmAction = confirmNone
	}
	return a, nil
}

func (a *App) confirmPrompt() string {
	switch a.confirmAction {
	case confirmStop:
		sel := a.sessions.Selected()
		if sel == nil {
			return "Stop? (y/n)"
		}
		if sel.Managed && sel.Status == session.StatusStopped {
			return fmt.Sprintf("Remove %s? (y/n)", sel.DisplayName())
		}
		if sel.Source == session.SourceSandbox && !sandboxIsActive(sel.Status) {
			return fmt.Sprintf("Clean %s (worktree + state)? (y/n)", sel.DisplayName())
		}
		return fmt.Sprintf("Stop %s? (y/n)", sel.DisplayName())
	case confirmResume:
		sel := a.sessions.Selected()
		if sel == nil {
			return "Resume? (y/n)"
		}
		return fmt.Sprintf("Resume %s? (y/n)", sel.DisplayName())
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
	default:
		return "(y/n)"
	}
}

func (a *App) executeStop() {
	sel := a.sessions.Selected()
	if sel == nil {
		return
	}

	// Managed session: two behaviors depending on current status.
	// - Running: kill the process but keep metadata so it shows as
	//   "stopped" and the user can resume it via `a`.
	// - Stopped: permanently remove the metadata (cleanup / forget).
	if sel.Managed {
		if sel.Status == session.StatusStopped {
			if a.ptyMgr != nil {
				_ = a.ptyMgr.RemoveMetadata(sel.ID)
			} else if metaPath, err := session.ManagedMetaPath(sel.ID); err == nil {
				_ = os.Remove(metaPath)
			}
			a.setFlash("Removed " + sel.DisplayName())
			return
		}

		// Running managed session. If this cs instance owns it, use the
		// manager's lifecycle. Otherwise SIGTERM the PID directly.
		if a.ptyMgr != nil {
			if _, owned := a.ptyMgr.Get(sel.ID); owned {
				if err := a.ptyMgr.Stop(context.Background(), sel.ID); err != nil {
					a.setFlash("Stop error: " + err.Error())
					return
				}
				a.setFlash("Stopped " + sel.DisplayName())
				return
			}
		}
		if sel.PID > 0 {
			if err := session.StopProcess(sel.PID); err != nil {
				a.setFlash("Stop error: " + err.Error())
				return
			}
			a.setFlash("Stopped " + sel.DisplayName())
			return
		}
		a.setFlash("Cannot stop " + sel.DisplayName())
		return
	}

	// Non-managed sandbox session: delegate cleanup to claude-sandbox
	// when it's not actively running claude. This removes both the
	// worktree and the state file so it disappears from the list.
	if sel.Source == session.SourceSandbox && !sandboxIsActive(sel.Status) {
		a.cleanSandboxSession(sel)
		return
	}

	// Non-managed session with a live process: SIGTERM it.
	if sel.PID > 0 {
		if err := session.StopProcess(sel.PID); err != nil {
			a.setFlash("Stop error: " + err.Error())
			return
		}
		a.setFlash("Stopped " + sel.DisplayName())
		return
	}

	a.setFlash("Stop not supported for " + string(sel.Source) + " sessions")
}

// sandboxIsActive reports whether a sandbox session is in a state where
// claude is currently running against it. We refuse to `clean` these to
// avoid yanking state from a live process.
func sandboxIsActive(status session.Status) bool {
	return status == session.StatusSpeccing || status == session.StatusRunning
}

// cleanSandboxSession shells out to `claude-sandbox clean --session <id>`
// with cmd.Dir pointed at the session's worktree so findRepoRoot() lands
// on the correct repo. Errors land in the status bar as a flash message.
func (a *App) cleanSandboxSession(sel *session.Session) {
	if sel.Dir == "" {
		a.setFlash("Cannot clean: session has no working directory")
		return
	}
	cmd := exec.Command("claude-sandbox", "clean", "--session", sel.ID)
	cmd.Dir = sel.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		a.setFlash("Clean error: " + msg)
		return
	}
	a.setFlash("Cleaned " + sel.DisplayName())
}

func (a *App) attachSession(sess *ptyPkg.ManagedSession) tea.Cmd {
	proxy := ptyPkg.NewProxy(sess)
	return tea.Exec(proxy, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
}

func (a *App) launchNewSession(kind newSessionType, dir string) tea.Cmd {
	if a.ptyMgr == nil {
		a.setFlash("PTY manager not initialized")
		return nil
	}

	var cmd *exec.Cmd
	var src session.Source
	switch kind {
	case newSessionSandbox:
		// claude-sandbox spec takes its working directory from cmd.Dir
		// (which must be inside a git repo). It only accepts --name and
		// --branch flags; there is no --dir.
		cmd = exec.Command("claude-sandbox", "spec")
		src = session.SourceSandbox
	default:
		cmd = exec.Command("claude")
		src = session.SourceNative
	}
	cmd.Dir = dir

	id := fmt.Sprintf("cs-%d", time.Now().UnixMilli())
	if err := a.ptyMgr.Spawn(context.Background(), id, cmd, dir, src); err != nil {
		a.setFlash("Spawn error: " + err.Error())
		return nil
	}

	sess, ok := a.ptyMgr.Get(id)
	if !ok {
		a.setFlash(fmt.Sprintf("session %s not found after spawn", id))
		return nil
	}
	return a.attachSession(sess)
}

func (a *App) executeResume() {
	sel := a.sessions.Selected()
	if sel == nil {
		return
	}

	if a.ptyMgr == nil {
		a.setFlash("PTY manager not initialized")
		return
	}

	// `claude --continue` resumes the most recent conversation in cmd.Dir.
	// We use this rather than `--resume <id>` because cs's session ID
	// (cs-<timestamp>) is not the real claude session UUID — claude would
	// open the picker filtered to that bogus ID and find nothing.
	cmd := exec.Command("claude", "--continue")
	cmd.Dir = sel.Dir
	if err := a.ptyMgr.Spawn(context.Background(), sel.ID, cmd, sel.Dir, sel.Source); err != nil {
		a.setFlash("Resume error: " + err.Error())
		return
	}
	a.setFlash("Resumed — press 'a' to attach")
}

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

	if err := a.ptyMgr.Spawn(context.Background(), id, cmd, sel.Dir, sel.Source); err != nil {
		a.setFlash("Stage error: " + err.Error())
		return
	}
	a.setFlash(fmt.Sprintf("Started %s — press 'a' to attach", args[0]))
}

func (a *App) setFlash(msg string) {
	a.flashMsg = msg
	a.flashExpiry = time.Now().Add(5 * time.Second)
}

func (a *App) updateLayout() {
	listWidth := a.width * 30 / 100
	detailWidth := a.width - listWidth
	contentHeight := a.height - 2
	a.sessions.SetSize(listWidth, contentHeight)
	a.detail.SetSize(detailWidth, contentHeight)
	a.statusbar.SetWidth(a.width)
}

func (a *App) updateDetail() {
	sel := a.sessions.Selected()
	if sel == nil {
		a.detail.SetSession(nil, nil, "", nil)
		return
	}
	activity := a.activities[sel.ID]
	lastMessage := a.lastMessages[sel.ID]
	convTail := a.conversationTails[sel.ID]
	a.detail.SetSession(sel, activity, lastMessage, convTail)
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg {
		if a.scanner == nil {
			return tickMsg{}
		}
		sessions, err := a.scanner.Scan(context.Background())
		if err != nil {
			return tickMsg{err: err}
		}

		activities := make(map[string][]scanner.ActivityEntry)
		lastMessages := make(map[string]string)
		conversationTails := make(map[string][]string)
		for _, s := range sessions {
			if s.LogPath != "" {
				if data, err := readLogTail(s.LogPath); err == nil {
					summary := scanner.ParseLog(data)
					activities[s.ID] = summary.RecentActivity
					if summary.LastMessage != "" {
						lastMessages[s.ID] = summary.LastMessage
					}
					if len(summary.ConversationTail) > 0 {
						conversationTails[s.ID] = summary.ConversationTail
					}
				}
			}
		}

		return tickMsg{
			sessions:          sessions,
			activities:        activities,
			lastMessages:      lastMessages,
			conversationTails: conversationTails,
		}
	})
}

func readLogTail(path string) ([]byte, error) {
	const maxBytes = 1024 * 1024
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.Size() <= maxBytes {
		return os.ReadFile(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxBytes)
	_, err = f.ReadAt(buf, info.Size()-maxBytes)
	if err != nil {
		return nil, err
	}
	// Skip to first complete line (the read may start mid-JSON-line).
	if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
		buf = buf[idx+1:]
	}
	return buf, nil
}
