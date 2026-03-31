package main

import (
	"fmt"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newLabelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "label [session] [description]",
		Short: "Set a task label on a session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if err := session.WriteLabel(sess.ID, args[1]); err != nil {
				return fmt.Errorf("write label: %w", err)
			}

			cmd.Printf("Labeled %s: %s\n", sess.ID, args[1])
			return nil
		},
	}
}
