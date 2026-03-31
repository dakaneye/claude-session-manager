package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

type nativeSession struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
}

// NativeSource discovers interactive Claude Code sessions.
type NativeSource struct {
	ClaudeDir string
}

func (n *NativeSource) Scan(_ context.Context) ([]session.Session, error) {
	claudeDir := n.ClaudeDir
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		claudeDir = filepath.Join(home, ".claude")
	}

	sessDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read native sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var ns nativeSession
		if err := json.Unmarshal(data, &ns); err != nil {
			continue
		}

		alive := isProcessAlive(ns.PID)
		status := session.StatusIdle
		if alive {
			status = session.StatusRunning
		}

		sess := session.Session{
			ID:        ns.SessionID,
			Source:    session.SourceNative,
			Status:    status,
			Dir:       ns.Cwd,
			PID:       ns.PID,
			StartedAt: time.UnixMilli(ns.StartedAt),
			Health:    session.HealthGreen,
			Name:      filepath.Base(ns.Cwd),
		}

		if alive {
			info, err := entry.Info()
			if err == nil {
				sess.LastActivity = info.ModTime()
			}
		}

		// Try to find and parse conversation JSONL for activity data.
		logPath := nativeLogPath(claudeDir, ns.Cwd, ns.SessionID)
		if _, statErr := os.Stat(logPath); statErr == nil {
			sess.LogPath = logPath
			if logData, readErr := os.ReadFile(logPath); readErr == nil {
				summary := ParseLog(logData)
				if !summary.LastActivity.IsZero() {
					sess.LastActivity = summary.LastActivity
				}
				sess.Diagnostics = EvaluateHealth(summary.RecentActivity, time.Now())
				sess.Health = session.WorstHealth(sess.Diagnostics)
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}

// nativeLogPath computes the conversation JSONL path for a native session.
// Claude encodes the cwd by replacing "/" with "-".
func nativeLogPath(claudeDir, cwd, sessionID string) string {
	encoded := strings.ReplaceAll(cwd, "/", "-")
	return filepath.Join(claudeDir, "projects", encoded, sessionID+".jsonl")
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
