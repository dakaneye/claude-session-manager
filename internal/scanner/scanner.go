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

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})

	return all, nil
}
