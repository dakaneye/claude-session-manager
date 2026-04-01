package main

import (
	"fmt"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newAttachCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "attach [session]",
		Short: "Attach to a managed session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if !sess.Managed {
				return fmt.Errorf("session %s is not managed by cs — use 'cs peek' to monitor", sess.ID)
			}

			if sess.Status != session.StatusRunning {
				return fmt.Errorf("session %s is not running (status: %s) — use 'cs resume' instead", sess.ID, sess.Status)
			}

			cmd.Println("Use 'cs' TUI and press 'a' to attach to managed sessions.")
			cmd.Println("Direct CLI attach requires the TUI to hold the PTY handle.")
			return nil
		},
	}
}
