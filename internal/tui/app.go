package tui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

const tickInterval = 3 * time.Second

type tickMsg struct {
	sessions   []session.Session
	activities map[string][]scanner.ActivityEntry
	err        error
}

// App is the root Bubbletea model.
type App struct {
	scanner    *scanner.Scanner
	sessions   sessionList
	detail     detailPane
	statusbar  statusBar
	width      int
	height     int
	activities map[string][]scanner.ActivityEntry
}

// NewApp creates a TUI application backed by the given scanner.
func NewApp(sc *scanner.Scanner) *App {
	return &App{
		scanner:    sc,
		sessions:   newSessionList(),
		detail:     newDetailPane(),
		statusbar:  newStatusBar(),
		activities: make(map[string][]scanner.ActivityEntry),
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
			a.updateDetail()
		}
		return a, a.tick()
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
	statusLine := a.statusbar.View()

	view := lipgloss.JoinVertical(lipgloss.Left, body, statusLine)

	v := tea.NewView(view)
	v.AltScreen = true
	return v
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	}
	return a, nil
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
		a.detail.SetSession(nil, nil)
		return
	}
	activity := a.activities[sel.ID]
	a.detail.SetSession(sel, activity)
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
		for _, s := range sessions {
			if s.LogPath != "" {
				if data, err := readLogTail(s.LogPath); err == nil {
					summary := scanner.ParseLog(data)
					activities[s.ID] = summary.RecentActivity
				}
			}
		}

		return tickMsg{sessions: sessions, activities: activities}
	})
}

func readLogTail(path string) ([]byte, error) {
	const maxBytes = 64 * 1024
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
	return buf, err
}
