package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newCleanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove stale native session entries",
		Long:  "Removes session files for dead native processes. Sandbox sessions should be cleaned with claude-sandbox clean.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessions, err := scanSessions()
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			sessDir := filepath.Join(home, ".claude", "sessions")

			cleaned := 0
			for _, s := range sessions {
				if s.Source != session.SourceNative {
					continue
				}
				if s.PID > 0 && !session.IsProcessAlive(s.PID) {
					pidFile := filepath.Join(sessDir, fmt.Sprintf("%d.json", s.PID))
					if err := os.Remove(pidFile); err == nil {
						cmd.Printf("Removed stale session: %s (PID %d)\n", s.DisplayName(), s.PID)
						cleaned++
					}
				}
			}

			if cleaned == 0 {
				cmd.Println("No stale sessions found.")
			} else {
				cmd.Printf("Cleaned %d stale sessions.\n", cleaned)
			}
			return nil
		},
	}

	return cmd
}
