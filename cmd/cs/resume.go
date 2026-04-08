package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newResumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [session]",
		Short: "Resume a stopped or dead session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			c := exec.Command("claude", "--resume", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Resuming session %s in %s...\n", sess.DisplayName(), sess.Dir)
			if err := c.Run(); err != nil {
				return fmt.Errorf("resume session: %w", err)
			}
			return nil
		},
	}
}
