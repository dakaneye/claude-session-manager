package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox session lifecycle",
	}

	cmd.AddCommand(newSandboxStageCommand("execute"))
	cmd.AddCommand(newSandboxStageCommand("ship"))
	return cmd
}

func newSandboxStageCommand(stage string) *cobra.Command {
	return &cobra.Command{
		Use:   stage + " [session]",
		Short: "Advance a sandbox session to the " + stage + " stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source != session.SourceSandbox {
				return fmt.Errorf("session %s is not a sandbox session", sess.ID)
			}

			c := exec.Command("claude-sandbox", stage, "--session", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Starting %s for %s...\n", stage, sess.DisplayName())
			if err := c.Run(); err != nil {
				return fmt.Errorf("%s sandbox session: %w", stage, err)
			}
			return nil
		},
	}
}
