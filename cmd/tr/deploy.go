package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/auth"
	"github.com/tinyraven/tinyraven/internal/branch"
	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/config"
	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/deploy"
)

// newDeployCmd builds `tr deploy`. main.go registers it (this file never edits
// main.go). It validates and applies the project's .datasource/.pipe files to
// the branch's ClickHouse workspace and registers the definitions in Redis
// (ADRs 0001, 0007, 0027).
func newDeployCmd() *cobra.Command {
	cfg := config.Load()
	var (
		allowBreaking bool
		projectDir    string
		branchFlag    string
		check         bool
	)
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Validate and apply .datasource/.pipe files to ClickHouse",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), config.Load(), projectDir, allowBreaking, branchFlag, check)
		},
	}
	cmd.Flags().BoolVar(&allowBreaking, "allow-breaking", false,
		"apply breaking schema changes via shadow table + EXCHANGE TABLES (ADR 0007)")
	cmd.Flags().StringVar(&projectDir, "project-dir", cfg.ProjectDir,
		"directory containing .datasource/.pipe files")
	cmd.Flags().StringVar(&branchFlag, "branch", "",
		"target workspace branch (default: current git branch -> tr_<branch>)")
	cmd.Flags().BoolVar(&check, "check", false,
		"dry run: validate + show the plan, apply nothing")
	return cmd
}

func runDeploy(ctx context.Context, cfg config.Config, dir string, allowBreaking bool, branchFlag string, check bool) error {
	// Resolve the workspace branch -> ClickHouse database (ADR 0007).
	b := branchFlag
	if b == "" {
		b, _ = branch.Current(ctx, dir) // best-effort; falls back to "main"
	}
	db := branch.DBName(b)
	fmt.Printf("→ workspace: %s (database %s)\n", b, db)

	ch, err := clickhouse.New(clickhouse.Config{
		HTTPURL:    cfg.CHHTTPURL,
		NativeAddr: cfg.CHNativeAddr,
		Database:   cfg.CHDatabase, // deploy creates + targets `db` via Options.Database
		User:       cfg.CHUser,
		Password:   cfg.CHPassword,
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()
	dsReg := datasource.NewRegistry(rdb)

	if check {
		fmt.Println("(--check: dry run, nothing will be applied)")
	}
	report, runErr := deploy.Run(ctx, dir, ch, dsReg, deploy.Options{
		AllowBreaking: allowBreaking,
		Database:      db,
		DryRun:        check,
		Tokens:        auth.NewStore(rdb), // materialize TOKEN "x" READ/APPEND (ADR 0030)
	})
	if report != nil {
		printReport(report)
	}
	return runErr
}

// printReport prints the Tinybird-style deploy summary to stdout.
func printReport(r *deploy.Report) {
	fmt.Printf("✓ Validated %d datasources, %d pipes\n", r.Datasources, r.Pipes)
	for _, name := range r.Created {
		fmt.Printf("✓ Created table %s\n", name)
	}
	for _, alter := range r.AltersApplied {
		fmt.Printf("✓ Applied migration: %s\n", alter)
	}
	for _, m := range r.BreakingApplied {
		fmt.Printf("✓ Breaking migration (shadow+EXCHANGE): %s\n", m)
	}
	for _, mv := range r.MaterializedViews {
		fmt.Printf("✓ Materialized view: %s\n", mv)
	}
	for _, tk := range r.Tokens {
		fmt.Printf("✓ Token materialized: %s\n", tk)
	}
	for _, b := range r.Breaking {
		fmt.Printf("✗ Breaking change (not applied; pass --allow-breaking): %s\n", b)
	}
	fmt.Printf("✓ Published %d endpoints\n", r.Pipes)
}
