package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/api"
	"github.com/tinyraven/tinyraven/internal/config"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the TinyRaven HTTP server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), config.Load())
		},
	}
}

func runServe(ctx context.Context, cfg config.Config) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// TODO(integration): replace stub deps with the real wiring once the
	// gatherer/pipe/datasource/clickhouse/auth packages land. Stubs let the
	// HTTP shell run and be exercised standalone.
	deps := wireStubs(cfg)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful drain on SIGTERM/SIGINT (ADR 0004).
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("tinyraven listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
