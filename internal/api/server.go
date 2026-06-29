// Package api is the HTTP layer: chi router, request/response glue, and
// middleware. It depends only on the model interfaces, so the gatherer, pipe,
// clickhouse and auth implementations can be developed independently and wired
// in at startup.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Deps are the concrete subsystem implementations the HTTP layer drives.
type Deps struct {
	Ingester  model.Ingester   // POST /v0/events
	Pipes     model.PipeRunner // GET  /v0/pipes/{name}.json
	Tokens    model.TokenStore // auth middleware
	RedisPing model.Pinger     // readiness
	CHPing    model.CHPinger   // readiness

	// MaxCompressedBytes caps the on-the-wire request body (ADR 0023). 0 -> 10MB.
	MaxCompressedBytes int64
}

type server struct {
	deps  Deps
	ready *readiness
}

// New builds the TinyRaven HTTP handler.
func New(deps Deps) http.Handler {
	if deps.MaxCompressedBytes == 0 {
		deps.MaxCompressedBytes = 10 << 20 // 10 MB
	}
	s := &server{
		deps:  deps,
		ready: newReadiness(deps.RedisPing, deps.CHPing, 2*time.Second),
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)

	// Health is unauthenticated (ADR 0024).
	r.Get("/health", s.handleLiveness)
	r.Get("/health/ready", s.handleReadiness)

	// /v0 — frozen Tinybird mirror (ADR 0029), behind bearer auth.
	r.Route("/v0", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/events", s.handleEvents)
		r.Get("/pipes/{name}.json", s.handlePipe)
	})

	return r
}
