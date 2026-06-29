package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/config"
	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/deploy"
)

// newDeployCmd builds `tr deploy`. main.go registers it (this file never edits
// main.go). It validates and applies the project's .datasource/.pipe files to
// ClickHouse and registers the definitions in Redis (ADRs 0001, 0027).
func newDeployCmd() *cobra.Command {
	cfg := config.Load()
	var (
		allowBreaking bool
		projectDir    string
	)
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Validate and apply .datasource/.pipe files to ClickHouse",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), config.Load(), projectDir, allowBreaking)
		},
	}
	cmd.Flags().BoolVar(&allowBreaking, "allow-breaking", false,
		"acknowledge breaking schema changes (still not applied — Phase 3, ADR 0007)")
	cmd.Flags().StringVar(&projectDir, "project-dir", cfg.ProjectDir,
		"directory containing .datasource/.pipe files")
	return cmd
}

func runDeploy(ctx context.Context, cfg config.Config, dir string, allowBreaking bool) error {
	ch, err := clickhouse.New(clickhouse.Config{
		HTTPURL:    cfg.CHHTTPURL,
		NativeAddr: cfg.CHNativeAddr,
		Database:   cfg.CHDatabase,
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

	report, runErr := deploy.Run(ctx, dir, ch, dsReg, deploy.Options{AllowBreaking: allowBreaking})
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
	for _, b := range r.Breaking {
		fmt.Printf("✗ Breaking change (not applied): %s\n", b)
	}
	fmt.Printf("✓ Published %d endpoints\n", r.Pipes)
}
