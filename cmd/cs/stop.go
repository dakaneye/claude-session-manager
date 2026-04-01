package main

import (
	"fmt"
	"os"

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

			if sess.Source == session.SourceSandbox {
				cmd.PrintErrln("Sandbox sessions should be stopped with: claude-sandbox stop " + sess.ID)
				return nil
			}

			if sess.PID <= 0 {
				return fmt.Errorf("cannot stop session %s (no PID)", sess.ID)
			}

			if err := session.StopProcess(sess.PID); err != nil {
				return fmt.Errorf("stop session %s: %w", sess.ID, err)
			}

			if sess.Managed {
				if metaPath, err := session.ManagedMetaPath(sess.ID); err == nil {
					_ = os.Remove(metaPath)
				}
			}

			cmd.Printf("Stopped %s (PID %d)\n", sess.DisplayName(), sess.PID)
			return nil
		},
	}
}
