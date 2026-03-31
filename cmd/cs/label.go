package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newLabelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "label [session] [description]",
		Short: "Set a task label on a session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			sess := findSession(sessions, args[0])
			if sess == nil {
				return fmt.Errorf("session not found: %s", args[0])
			}

			home, _ := os.UserHomeDir()
			labelDir := filepath.Join(home, ".claude", "session-labels")
			if err := os.MkdirAll(labelDir, 0o755); err != nil {
				return fmt.Errorf("create label dir: %w", err)
			}

			label := map[string]string{
				"session_id": sess.ID,
				"label":      args[1],
			}
			data, _ := json.MarshalIndent(label, "", "  ")
			labelPath := filepath.Join(labelDir, sess.ID+".json")
			if err := os.WriteFile(labelPath, data, 0o644); err != nil {
				return fmt.Errorf("write label: %w", err)
			}

			cmd.Printf("Labeled %s: %s\n", sess.ID, args[1])
			return nil
		},
	}
}
