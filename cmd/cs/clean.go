package main

import (
	"context"
	"fmt"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newCleanCommand() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove completed or failed sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			cleaned := 0
			for _, s := range sessions {
				if !all && s.Status != session.StatusSuccess && s.Status != session.StatusFailed {
					continue
				}
				switch s.Source {
				case session.SourceSandbox:
					cmd.Printf("Clean sandbox session %s with: claude-sandbox clean --session %s\n", s.ID, s.ID)
				case session.SourceNative:
					cmd.Printf("Native session %s (pid %d) - remove stale session file manually if process is dead\n", s.ID, s.PID)
				}
				cleaned++
			}

			if cleaned == 0 {
				cmd.Println("Nothing to clean.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Clean all sessions, not just completed/failed")
	return cmd
}
