package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/branch"
)

func newLocalCmd() *cobra.Command {
	local := &cobra.Command{
		Use:   "local",
		Short: "Manage the local dev stack (ClickHouse + Redis + TinyRaven)",
	}
	var branchFlag string
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the local dev stack via Docker Compose",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --branch isolates data in its own ClickHouse DB tr_<branch> (ADR 0007).
			b := branchFlag
			if b == "" {
				b, _ = branch.Current(cmd.Context(), ".")
			}
			db := branch.DBName(b)
			os.Setenv("TR_CLICKHOUSE_DB", db) // compose interpolates ${TR_CLICKHOUSE_DB}
			fmt.Printf("→ branch %s -> database %s\n", b, db)
			return compose(cmd.Context(), "up", "-d")
		},
	}
	start.Flags().StringVar(&branchFlag, "branch", "",
		"isolate this branch's data in ClickHouse db tr_<branch> (default: current git branch)")
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
