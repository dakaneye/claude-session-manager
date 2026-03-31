package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newLsCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessions, err := scanSessions()
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			if jsonOutput {
				return printJSON(cmd, sessions)
			}
			return printTable(cmd, sessions)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func printJSON(cmd *cobra.Command, sessions []session.Session) error {
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	cmd.Println(string(data))
	return nil
}

func printTable(cmd *cobra.Command, sessions []session.Session) error {
	if len(sessions) == 0 {
		cmd.Println("No sessions found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HEALTH\tNAME\tSOURCE\tSTATUS\tDIR")
	for _, s := range sessions {
		dot := healthSymbol(s.Health)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", dot, s.DisplayName(), s.Source, s.Status, s.Dir)
	}
	return w.Flush()
}

func healthSymbol(h session.Health) string {
	switch h {
	case session.HealthGreen:
		return "●"
	case session.HealthYellow:
		return "◉"
	case session.HealthRed:
		return "✖"
	default:
		return "○"
	}
}
