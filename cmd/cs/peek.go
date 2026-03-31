package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newPeekCommand() *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "peek [session]",
		Short: "Tail session log output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.LogPath == "" {
				return fmt.Errorf("no log path for session %s (source: %s)", sess.ID, sess.Source)
			}

			data, err := os.ReadFile(sess.LogPath)
			if err != nil {
				return fmt.Errorf("read log: %w", err)
			}

			summary := scanner.ParseLog(data)
			maxEntries := lines
			start := 0
			if len(summary.RecentActivity) > maxEntries {
				start = len(summary.RecentActivity) - maxEntries
			}
			for _, a := range summary.RecentActivity[start:] {
				ts := ""
				if !a.Time.IsZero() {
					ts = a.Time.Format(time.TimeOnly)
				}
				errMark := ""
				if a.IsError {
					errMark = " [ERROR]"
				}
				cmd.Printf("%s  %-6s  %s%s\n", ts, a.Tool, a.Detail, errMark)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "n", 20, "Number of recent activities to show")
	return cmd
}

func findSession(sessions []session.Session, query string) *session.Session {
	for i, s := range sessions {
		if s.ID == query || s.Name == query {
			return &sessions[i]
		}
	}
	return nil
}
