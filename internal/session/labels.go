package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type labelData struct {
	Label string `json:"label"`
}

// WriteLabel persists a label for the given session ID.
func WriteLabel(sessionID, label string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".claude", "session-labels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create label dir: %w", err)
	}
	data, err := json.Marshal(labelData{Label: label})
	if err != nil {
		return fmt.Errorf("marshal label: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, sessionID+".json"), data, 0o644)
}

// ReadLabel returns the persisted label for a session, or "" if none exists.
func ReadLabel(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "session-labels", sessionID+".json"))
	if err != nil {
		return ""
	}
	var ld labelData
	if err := json.Unmarshal(data, &ld); err != nil {
		return ""
	}
	return ld.Label
}
