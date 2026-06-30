// Package api is the HTTP layer: chi router, request/response glue, and
// middleware. It depends only on the model interfaces and a few injected
// http.Handlers/middlewares, so the subsystem implementations can be developed
// independently and wired in at startup.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Deps are the concrete subsystem implementations the HTTP layer drives. The
// http.Handler / middleware / func fields are optional (nil-checked) so the
// server degrades gracefully if a piece isn't wired.
type Deps struct {
	Ingester    model.Ingester           // POST /v0/events
	Pipes       model.PipeRunner         // GET  /v0/pipes/{name}.json
	Datasources model.DatasourceRegistry // GET  /v0/datasources (optional)
	Tokens      model.TokenStore         // auth middleware
	RedisPing   model.Pinger             // readiness
	CHPing      model.Pinger             // readiness

	// Phase 2 add-ons (optional).
	SQLProxy          http.Handler                      // GET/POST /v0/sql (ADR 0011)
	MetricsHandler    http.Handler                      // GET /v0/metrics (Prometheus)
	MetricsMiddleware func(http.Handler) http.Handler   // per-request metrics
	RateLimit         func(http.Handler) http.Handler   // per-token limiter on pipes (ADR 0015)
	OpenAPI           func() []byte                     // GET /v0/openapi.json (ADR 0017)
	IngestObserver    func(successful, quarantined int) // events -> metrics hook
	DocsUI            http.Handler                      // /tr/v1/docs page (ADR 0017)
	DocsEnabled       bool                              // serve the docs UI (off by default)

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
	if deps.MetricsMiddleware != nil {
		r.Use(deps.MetricsMiddleware)
	}

	// Unauthenticated: health (ADR 0024) + metrics scrape endpoint.
	r.Get("/health", s.handleLiveness)
	r.Get("/health/ready", s.handleReadiness)
	if deps.MetricsHandler != nil {
		r.Handle("/v0/metrics", deps.MetricsHandler)
	}

	// /tr/v1 — TinyRaven-native namespace (ADR 0029). Docs UI is off by default
	// (ADR 0017); when on, the UI + an unauthenticated spec it can fetch are
	// served here (the /v0 spec stays bearer-gated for API clients).
	if deps.DocsEnabled && deps.DocsUI != nil && deps.OpenAPI != nil {
		r.Handle("/tr/v1/docs", deps.DocsUI)
		r.Get("/tr/v1/openapi.json", s.handleOpenAPI)
	}

	// /v0 — frozen Tinybird mirror (ADR 0029), behind bearer auth.
	r.Route("/v0", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/events", s.handleEvents)
		if deps.SQLProxy != nil {
			// Raw SQL is powerful -> ADMIN only (ADR 0011 + 0005).
			r.With(s.adminOnly).Handle("/sql", deps.SQLProxy) // GET + POST
		}
		if deps.Datasources != nil {
			// Listing every datasource schema is privileged -> ADMIN only.
			r.With(s.adminOnly).Get("/datasources", s.handleListDatasources)
		}
		if deps.OpenAPI != nil {
			r.Get("/openapi.json", s.handleOpenAPI)
		}
		// Pipe reads carry the per-token rate limiter (ADR 0015).
		r.Group(func(r chi.Router) {
			if deps.RateLimit != nil {
				r.Use(deps.RateLimit)
			}
			r.Get("/pipes/{name}.json", s.handlePipe)
		})
	})

	return r
}

// handleOpenAPI serves the runtime-generated OpenAPI spec (ADR 0017).
func (s *server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.deps.OpenAPI())
}
