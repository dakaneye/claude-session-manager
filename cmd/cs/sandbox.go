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

	cmd.AddCommand(newSandboxExecuteCommand())
	cmd.AddCommand(newSandboxShipCommand())
	return cmd
}

func newSandboxExecuteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "execute [session]",
		Short: "Advance a sandbox session to the execute stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source != session.SourceSandbox {
				return fmt.Errorf("session %s is not a sandbox session", sess.ID)
			}

			c := exec.Command("claude-sandbox", "execute", "--session", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Starting execute for %s...\n", sess.DisplayName())
			if err := c.Run(); err != nil {
				return fmt.Errorf("execute sandbox session: %w", err)
			}
			return nil
		},
	}
}

func newSandboxShipCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ship [session]",
		Short: "Advance a sandbox session to the ship stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source != session.SourceSandbox {
				return fmt.Errorf("session %s is not a sandbox session", sess.ID)
			}

			c := exec.Command("claude-sandbox", "ship", "--session", sess.ID)
			c.Dir = sess.Dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			cmd.Printf("Starting ship for %s...\n", sess.DisplayName())
			if err := c.Run(); err != nil {
				return fmt.Errorf("ship sandbox session: %w", err)
			}
			return nil
		},
	}
}
