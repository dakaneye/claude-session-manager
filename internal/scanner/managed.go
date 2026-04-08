package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// ManagedSource discovers sessions launched and owned by cs.
type ManagedSource struct {
	StateDir string // ~/.claude/cs-sessions/
}

func (m *ManagedSource) Scan(_ context.Context) ([]session.Session, error) {
	entries, err := os.ReadDir(m.StateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read managed sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(m.StateDir, entry.Name()))
		if err != nil {
			continue
		}

		var meta session.ManagedMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		alive := session.IsProcessAlive(meta.PID)
		status := session.StatusStopped
		if alive {
			status = session.StatusRunning
		}

		source := meta.Source
		if source == "" {
			source = session.SourceNative
		}

		sess := session.Session{
			ID:        meta.ID,
			Source:    source,
			Status:    status,
			Dir:       meta.Dir,
			PID:       meta.PID,
			StartedAt: meta.CreatedAt,
			Health:    session.HealthGreen,
			Name:      filepath.Base(meta.Dir),
			Managed:   true,
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}
