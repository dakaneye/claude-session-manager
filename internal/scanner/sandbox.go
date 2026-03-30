package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// sandboxSession mirrors the JSON structure from claude-sandbox state files.
type sandboxSession struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	WorktreePath string    `json:"worktree_path"`
	Branch       string    `json:"branch"`
	Status       string    `json:"status"`
	LogPath      string    `json:"log_path"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// SandboxSource discovers sessions from claude-sandbox state directories.
type SandboxSource struct {
	RepoPaths []string
	LogDir    string
}

func (s *SandboxSource) Scan(_ context.Context) ([]session.Session, error) {
	var sessions []session.Session
	for _, repo := range s.RepoPaths {
		found, err := s.scanRepo(repo)
		if err != nil {
			return nil, fmt.Errorf("scan sandbox repo %s: %w", repo, err)
		}
		sessions = append(sessions, found...)
	}
	return sessions, nil
}

func (s *SandboxSource) scanRepo(repoPath string) ([]session.Session, error) {
	sessDir := filepath.Join(repoPath, ".claude-sandbox", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Skip symlinks (named session aliases).
		fullPath := filepath.Join(sessDir, entry.Name())
		fi, err := os.Lstat(fullPath)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		var ss sandboxSession
		if err := json.Unmarshal(data, &ss); err != nil {
			continue
		}

		sess := session.Session{
			ID:        ss.ID,
			Name:      ss.Name,
			Source:    session.SourceSandbox,
			Status:    mapSandboxStatus(ss.Status),
			Dir:       ss.WorktreePath,
			Branch:    ss.Branch,
			StartedAt: ss.StartedAt,
			LogPath:   ss.LogPath,
		}
		if sess.StartedAt.IsZero() {
			sess.StartedAt = ss.CreatedAt
		}

		// Parse log for activity data.
		logPath := s.resolveLogPath(ss)
		if logData, err := os.ReadFile(logPath); err == nil {
			summary := ParseLog(logData)
			sess.LastActivity = summary.LastActivity
			sess.Diagnostics = EvaluateHealth(summary.RecentActivity, time.Now())
			sess.Health = session.WorstHealth(sess.Diagnostics)
		} else {
			sess.Health = session.HealthGreen
		}

		// Try to extract task from PLAN.md.
		if ss.WorktreePath != "" {
			if task := readPlanTitle(ss.WorktreePath); task != "" {
				sess.Task = task
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}

func (s *SandboxSource) resolveLogPath(ss sandboxSession) string {
	if s.LogDir != "" {
		return filepath.Join(s.LogDir, ss.ID+".log")
	}
	if ss.LogPath != "" {
		return ss.LogPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "sandbox-sessions", ss.ID+".log")
}

func readPlanTitle(worktreePath string) string {
	data, err := os.ReadFile(filepath.Join(worktreePath, "PLAN.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 5) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func mapSandboxStatus(status string) session.Status {
	switch status {
	case "speccing":
		return session.StatusSpeccing
	case "ready":
		return session.StatusReady
	case "running":
		return session.StatusRunning
	case "success":
		return session.StatusSuccess
	case "failed":
		return session.StatusFailed
	case "blocked":
		return session.StatusBlocked
	default:
		return session.Status(status)
	}
}
