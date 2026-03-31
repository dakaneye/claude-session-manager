package tui

import tea "charm.land/bubbletea/v2"

type keyAction int

const (
	keyNone keyAction = iota
	keyUp
	keyDown
	keyPeek
	keyNew
	keyStop
	keyClean
	keyLabel
	keyAttach
	keyHelp
	keyQuit
)

func parseKey(msg tea.KeyPressMsg) keyAction {
	// Check special keys by code first.
	switch msg.Code {
	case tea.KeyEnter:
		return keyPeek
	case tea.KeyUp:
		return keyUp
	case tea.KeyDown:
		return keyDown
	case tea.KeyEscape:
		return keyNone
	}
	// Then check printable keys by text.
	switch msg.String() {
	case "k":
		return keyUp
	case "j":
		return keyDown
	case "n":
		return keyNew
	case "s":
		return keyStop
	case "l":
		return keyLabel
	case "a":
		return keyAttach
	case "?":
		return keyHelp
	case "q":
		return keyQuit
	case "ctrl+c":
		return keyQuit
	default:
		return keyNone
	}
}
