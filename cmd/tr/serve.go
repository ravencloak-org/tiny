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

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/api"
	"github.com/tinyraven/tinyraven/internal/auth"
	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/config"
	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/gatherer"
	"github.com/tinyraven/tinyraven/internal/pipe"
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

	// Shutdown context: SIGTERM/SIGINT triggers graceful drain (ADR 0004).
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Backing stores.
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

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

	// Registries + subsystems.
	dsReg := datasource.NewRegistry(rdb)
	pipeReg := pipe.NewRegistry()
	executor := pipe.NewExecutor(ch, pipeReg, dsReg)
	gath := gatherer.New(ch, dsReg, gatherer.WithLogger(log))
	tokens := auth.NewStore(rdb)

	if cfg.AdminToken != "" {
		if err := tokens.Bootstrap(ctx, cfg.AdminToken); err != nil {
			log.Warn("admin token bootstrap failed (Redis down?)", "err", err)
		} else {
			log.Info("bootstrapped ADMIN token")
		}
	}

	// Load project files; non-fatal so the server still serves /health while a
	// dependency comes up.
	proj := &project{ch: ch, dsReg: dsReg, pipeReg: pipeReg, log: log}
	if err := proj.apply(ctx, cfg.ProjectDir); err != nil {
		log.Warn("initial project load failed; readiness will report not-ready", "err", err)
	}
	go proj.watch(ctx, cfg.ProjectDir) // dev-only hot reload (ADR 0020)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.New(api.Deps{
			Ingester:  gath,
			Pipes:     executor,
			Tokens:    tokens,
			RedisPing: tokens, // auth.Store.Ping pings Redis
			CHPing:    ch,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

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
		if err := gath.Close(shutdownCtx); err != nil { // drain buffered events first (ADR 0004)
			log.Error("gatherer drain failed", "err", err)
		}
		return srv.Shutdown(shutdownCtx)
	}
}
