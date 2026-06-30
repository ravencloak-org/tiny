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
	"github.com/tinyraven/tinyraven/internal/metrics"
	"github.com/tinyraven/tinyraven/internal/openapi"
	"github.com/tinyraven/tinyraven/internal/pipe"
	"github.com/tinyraven/tinyraven/internal/pipestats"
	"github.com/tinyraven/tinyraven/internal/ratelimit"
	"github.com/tinyraven/tinyraven/internal/sqlproxy"
)

// Pipe rate limit comes from config (TR_PIPE_RATE_LIMIT, default 100; 0 disables).
// ponytail: global per-token default; per-pipe RATE_LIMIT + a shared
// (httprate-redis) store are the upgrades (ADR 0015 / 0031).

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
		ROUser:     cfg.CHROUser,
		ROPassword: cfg.CHROPassword,
	})
	if err != nil {
		return err
	}
	// Provision the read-only user before serving (Ping/readiness use it; ADR 0011).
	if cfg.CHROUser != "" {
		if err := ch.EnsureReadonlyUser(ctx, cfg.CHROUser, cfg.CHROPassword); err != nil {
			log.Warn("could not ensure read-only CH user", "err", err)
		} else {
			log.Info("ensured read-only ClickHouse user", "user", cfg.CHROUser)
		}
	}

	// Registries + subsystems.
	dsReg := datasource.NewRegistry(rdb)
	pipeReg := pipe.NewRegistry()
	gath := gatherer.New(ch, dsReg, gatherer.WithLogger(log))
	tokens := auth.NewStore(rdb)

	// Observability + add-ons (Phase 2).
	mx := metrics.New()
	stats := pipestats.New(ch)
	if err := ch.EnsureTable(ctx, stats.Schema()); err != nil { // pipe_stats table (ADR 0014)
		log.Warn("could not ensure pipe_stats table", "err", err)
	}
	executor := pipe.NewExecutor(ch, pipeReg, dsReg, stats)

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
			Ingester:          gath,
			Pipes:             executor,
			Datasources:       dsReg,
			Tokens:            tokens,
			RedisPing:         tokens, // auth.Store.Ping pings Redis
			CHPing:            ch,
			SQLProxy:          sqlproxy.New(ch),
			MetricsHandler:    mx.Handler(),
			MetricsMiddleware: mx.Middleware,
			RateLimit: ratelimit.PerPipe(cfg.PipeRateLimit, func(p string) int {
				if pp, ok := pipeReg.Get(p); ok && pp.Endpoint != nil {
					return pp.Endpoint.RateLimit // per-pipe RATE_LIMIT overrides default (ADR 0015)
				}
				return 0
			}),
			OpenAPI:        func() []byte { return openapi.Generate(pipeReg.List()) },
			IngestObserver: mx.IngestObserved,
			DocsUI:         api.DocsHandler(),
			DocsEnabled:    cfg.DocsEnabled,
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
		if err := stats.Close(shutdownCtx); err != nil { // drain pipe_stats (ADR 0014)
			log.Error("pipe_stats drain failed", "err", err)
		}
		return srv.Shutdown(shutdownCtx)
	}
}
