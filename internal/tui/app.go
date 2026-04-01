package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	mode          inputMode
	labelInput    string
	confirmAction confirmAction
	flashMsg      string
	flashExpiry   time.Time
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
		return a, nil
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

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	var statusLine string
	switch a.mode {
	case modeLabel:
		statusLine = a.statusbar.RenderInput("Label: ", a.labelInput)
	case modeConfirm:
		statusLine = a.statusbar.RenderConfirm(a.confirmPrompt())
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
		return a, a.launchClaude()
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
	if sel.Source == session.SourceNative && sel.PID > 0 {
		proc, err := os.FindProcess(sel.PID)
		if err != nil {
			a.setFlash("Process not found")
			return
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			a.setFlash("Stop error: " + err.Error())
			return
		}
		// Remove the session file so it disappears from the list.
		home, _ := os.UserHomeDir()
		pidFile := filepath.Join(home, ".claude", "sessions", fmt.Sprintf("%d.json", sel.PID))
		_ = os.Remove(pidFile)
		a.setFlash("Stopped PID " + fmt.Sprint(sel.PID))
	} else {
		a.setFlash("Stop not supported for " + string(sel.Source) + " sessions")
	}
}

func (a *App) attachSession(sess *ptyPkg.ManagedSession) tea.Cmd {
	proxy := ptyPkg.NewProxy(sess)
	return tea.Exec(proxy, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
}

func (a *App) launchClaude() tea.Cmd {
	if a.ptyMgr == nil {
		c := exec.Command("claude")
		return tea.ExecProcess(c, func(err error) tea.Msg {
			return execFinishedMsg{err: err}
		})
	}

	dir, _ := os.Getwd()
	id := fmt.Sprintf("cs-%d", time.Now().UnixMilli())
	cmd := exec.Command("claude")
	cmd.Dir = dir

	if err := a.ptyMgr.Spawn(id, cmd, dir); err != nil {
		return func() tea.Msg {
			return execFinishedMsg{err: err}
		}
	}

	sess, _ := a.ptyMgr.Get(id)
	return a.attachSession(sess)
}

func (a *App) executeResume() {
	sel := a.sessions.Selected()
	if sel == nil {
		return
	}

	if a.ptyMgr != nil {
		cmd := exec.Command("claude", "--resume", sel.ID)
		cmd.Dir = sel.Dir
		if err := a.ptyMgr.Spawn(sel.ID, cmd, sel.Dir); err != nil {
			a.setFlash("Resume error: " + err.Error())
			return
		}
		a.setFlash("Resumed — press 'a' to attach")
		return
	}
	a.setFlash("PTY manager not initialized")
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

	if err := a.ptyMgr.Spawn(id, cmd, sel.Dir); err != nil {
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
