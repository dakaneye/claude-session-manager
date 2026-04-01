package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	ptyPkg "github.com/dakaneye/claude-session-manager/internal/pty"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/dakaneye/claude-session-manager/internal/tui"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cs",
		Short: "Manage multiple Claude Code sessions",
		Long:  "TUI + CLI for managing interactive and autonomous Claude Code sessions.",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, _ := os.UserHomeDir()
			stateDir := filepath.Join(home, ".claude", "cs-sessions")
			ptyMgr := ptyPkg.NewManager(stateDir)

			sc := buildScanner()
			app := tui.NewApp(sc, ptyMgr)
			p := tea.NewProgram(app)
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("run TUI: %w", err)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(newLsCommand())
	cmd.AddCommand(newPeekCommand())
	cmd.AddCommand(newStopCommand())
	cmd.AddCommand(newLabelCommand())
	cmd.AddCommand(newCleanCommand())
	cmd.AddCommand(newNewCommand())
	cmd.AddCommand(newVersionCommand())

	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("cs", version)
		},
	}
}

func scanSessions() ([]session.Session, error) {
	sc := buildScanner()
	return sc.Scan(context.Background())
}

func resolveSession(query string) (*session.Session, []session.Session, error) {
	sessions, err := scanSessions()
	if err != nil {
		return nil, nil, err
	}
	sess := findSession(sessions, query)
	if sess == nil {
		return nil, sessions, fmt.Errorf("session not found: %s", query)
	}
	return sess, sessions, nil
}

func buildScanner() *scanner.Scanner {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	return &scanner.Scanner{
		Sources: []scanner.SessionSource{
			&scanner.ManagedSource{
				StateDir: filepath.Join(home, ".claude", "cs-sessions"),
			},
			&scanner.SandboxSource{
				RepoPaths: []string{cwd},
			},
			&scanner.NativeSource{
				ClaudeDir: home + "/.claude",
			},
		},
	}
}
