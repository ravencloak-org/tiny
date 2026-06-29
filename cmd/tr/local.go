package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newLocalCmd() *cobra.Command {
	local := &cobra.Command{
		Use:   "local",
		Short: "Manage the local dev stack (ClickHouse + Redis + TinyRaven)",
	}
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the local dev stack via Docker Compose",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return compose(cmd.Context(), "up", "-d")
		},
	}
	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop the local dev stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return compose(cmd.Context(), "down")
		},
	}
	local.AddCommand(start, stop)
	return local
}

// compose shells out to `docker compose` against the repo's docker-compose.yml.
func compose(ctx context.Context, args ...string) error {
	full := append([]string{"compose"}, args...)
	c := exec.CommandContext(ctx, "docker", full...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}
