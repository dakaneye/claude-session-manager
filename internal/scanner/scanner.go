package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// SessionSource discovers sessions from a single source.
type SessionSource interface {
	Scan(ctx context.Context) ([]session.Session, error)
}

// Scanner aggregates sessions from multiple sources.
type Scanner struct {
	Sources []SessionSource
}

// Scan collects sessions from all sources, sorted by last activity (most recent first).
func (s *Scanner) Scan(ctx context.Context) ([]session.Session, error) {
	var all []session.Session
	for _, src := range s.Sources {
		sessions, err := src.Scan(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		all = append(all, sessions...)
	}

	// Apply persisted labels.
	applyLabels(all)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})

	return all, nil
}

// applyLabels reads session labels from ~/.claude/session-labels/ and sets Task.
func applyLabels(sessions []session.Session) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	labelDir := filepath.Join(home, ".claude", "session-labels")

	for i := range sessions {
		labelPath := filepath.Join(labelDir, sessions[i].ID+".json")
		data, err := os.ReadFile(labelPath)
		if err != nil {
			continue
		}
		var label struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(data, &label); err != nil {
			continue
		}
		if label.Label != "" {
			sessions[i].Task = label.Label
		}
	}
}
