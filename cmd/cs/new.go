package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newNewCommand() *cobra.Command {
	var (
		sandbox bool
		dir     string
		name    string
	)

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new Claude session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				var err error
				dir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			if sandbox {
				return launchSandboxSession(cmd, dir, name)
			}
			return launchInteractiveSession(dir)
		},
	}

	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "Create a sandbox (autonomous) session")
	cmd.Flags().StringVar(&dir, "dir", "", "Working directory (default: cwd)")
	cmd.Flags().StringVar(&name, "name", "", "Session name (sandbox only)")
	return cmd
}

func launchSandboxSession(_ *cobra.Command, dir, name string) error {
	args := []string{"spec"}
	if name != "" {
		args = append(args, "--name", name)
	}
	args = append(args, "--dir", dir)

	c := exec.Command("claude-sandbox", args...)
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("launch claude-sandbox spec: %w", err)
	}
	return nil
}

func launchInteractiveSession(dir string) error {
	c := exec.Command("claude")
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("launch claude: %w", err)
	}
	return nil
}
