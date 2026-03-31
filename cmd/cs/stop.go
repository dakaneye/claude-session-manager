package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [session]",
		Short: "Stop a running session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, _, err := resolveSession(args[0])
			if err != nil {
				return err
			}

			if sess.Source == session.SourceNative && sess.PID > 0 {
				proc, err := os.FindProcess(sess.PID)
				if err != nil {
					return fmt.Errorf("find process %d: %w", sess.PID, err)
				}
				if err := proc.Signal(syscall.SIGTERM); err != nil {
					return fmt.Errorf("signal process %d: %w", sess.PID, err)
				}
				cmd.Printf("Sent SIGTERM to PID %d (%s)\n", sess.PID, sess.ID)
				return nil
			}

			if sess.Source == session.SourceSandbox {
				cmd.PrintErrln("Sandbox sessions should be stopped with: claude-sandbox stop " + sess.ID)
				return nil
			}

			return fmt.Errorf("cannot stop session %s (source: %s)", sess.ID, sess.Source)
		},
	}
}
