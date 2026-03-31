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
	keyHelp
	keyQuit
)

func parseKey(msg tea.KeyPressMsg) keyAction {
	switch msg.String() {
	case "up", "k":
		return keyUp
	case "down", "j":
		return keyDown
	case "enter":
		return keyPeek
	case "n":
		return keyNew
	case "s":
		return keyStop
	case "c":
		return keyClean
	case "l":
		return keyLabel
	case "?":
		return keyHelp
	case "q", "ctrl+c":
		return keyQuit
	default:
		return keyNone
	}
}
