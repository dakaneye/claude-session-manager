package scanner

import (
	"context"
	"fmt"
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

	all = deduplicateByPID(all)

	// Apply persisted labels.
	applyLabels(all)

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})

	return all, nil
}

// deduplicateByPID removes duplicate sessions by PID, preferring managed
// sessions but merging health/log data from the native entry.
func deduplicateByPID(sessions []session.Session) []session.Session {
	seen := make(map[int]int) // PID -> index in result
	var result []session.Session

	for _, s := range sessions {
		if s.PID <= 0 {
			result = append(result, s)
			continue
		}
		if idx, exists := seen[s.PID]; exists {
			if s.Managed && !result[idx].Managed {
				existing := result[idx]
				result[idx] = s
				// Merge health/log data from the native entry.
				if result[idx].LogPath == "" {
					result[idx].LogPath = existing.LogPath
				}
				if result[idx].Health == session.HealthGreen && existing.Health != session.HealthGreen {
					result[idx].Health = existing.Health
					result[idx].Diagnostics = existing.Diagnostics
				}
				if result[idx].LastActivity.IsZero() {
					result[idx].LastActivity = existing.LastActivity
				}
			}
			continue
		}
		seen[s.PID] = len(result)
		result = append(result, s)
	}

	return result
}

// applyLabels reads session labels from ~/.claude/session-labels/ and sets Task.
func applyLabels(sessions []session.Session) {
	for i := range sessions {
		if label := session.ReadLabel(sessions[i].ID); label != "" {
			sessions[i].Task = label
		}
	}
}
