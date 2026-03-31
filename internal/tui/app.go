package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	confirmClean
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
	confirmCount  int // for clean: number of sessions to clean
	flashMsg      string
	flashExpiry   time.Time
}

// NewApp creates a TUI application backed by the given scanner.
func NewApp(sc *scanner.Scanner) *App {
	return &App{
		scanner:      sc,
		sessions:     newSessionList(),
		detail:       newDetailPane(),
		statusbar:    newStatusBar(),
		activities:   make(map[string][]scanner.ActivityEntry),
		lastMessages: make(map[string]string),
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
		name := sel.Name
		if name == "" {
			name = sel.ID
		}
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
	case keyClean:
		count := a.countCleanable()
		if count > 0 {
			a.mode = modeConfirm
			a.confirmAction = confirmClean
			a.confirmCount = count
		} else {
			a.setFlash("No sessions to clean")
		}
	case keyNew:
		return a, a.launchClaude()
	case keyAttach:
		if sel := a.sessions.Selected(); sel != nil && sel.Source == session.SourceNative {
			a.setFlash(fmt.Sprintf("Session running in: %s (PID %d)", sel.Dir, sel.PID))
		} else if sel != nil {
			a.setFlash("Attach only works for native sessions")
		}
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
			if err := writeLabel(sel.ID, a.labelInput); err != nil {
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
		case confirmClean:
			a.executeClean()
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
		name := sel.Name
		if name == "" {
			name = sel.ID
		}
		return fmt.Sprintf("Stop %s? (y/n)", name)
	case confirmClean:
		return fmt.Sprintf("Clean %d sessions? (y/n)", a.confirmCount)
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

func (a *App) countCleanable() int {
	count := 0
	for _, s := range a.sessions.sessions {
		if isCleanable(s) {
			count++
		}
	}
	return count
}

func isCleanable(s session.Session) bool {
	switch s.Status {
	case session.StatusSuccess, session.StatusFailed:
		return true
	case session.StatusIdle:
		// Native idle sessions with dead process are cleanable.
		if s.Source == session.SourceNative && s.PID > 0 {
			return !isAlive(s.PID)
		}
		return false
	default:
		return false
	}
}

func isAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (a *App) executeClean() {
	cleaned := 0
	home, err := os.UserHomeDir()
	if err != nil {
		a.setFlash("Clean error: " + err.Error())
		return
	}
	sessDir := filepath.Join(home, ".claude", "sessions")

	for _, s := range a.sessions.sessions {
		if !isCleanable(s) {
			continue
		}
		if s.Source == session.SourceNative && s.PID > 0 {
			pidFile := filepath.Join(sessDir, fmt.Sprintf("%d.json", s.PID))
			if err := os.Remove(pidFile); err == nil {
				cleaned++
			}
		}
	}
	a.setFlash(fmt.Sprintf("Cleaned %d sessions", cleaned))
}

func (a *App) launchClaude() tea.Cmd {
	c := exec.Command("claude")
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
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
	const maxBytes = 512 * 1024
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

// sessionLabel is the JSON structure for persisted session labels.
type sessionLabel struct {
	Label string `json:"label"`
}

func writeLabel(sessionID, label string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".claude", "session-labels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create label dir: %w", err)
	}
	data, err := json.Marshal(sessionLabel{Label: label})
	if err != nil {
		return fmt.Errorf("marshal label: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, sessionID+".json"), data, 0o644)
}
