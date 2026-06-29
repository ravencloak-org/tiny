// Package metrics owns the Prometheus registry and the collectors exposed at
// GET /v0/metrics (Phase 2). It provides a chi-compatible middleware that
// records per-request count + duration keyed by route *pattern* (never the raw
// path, to keep label cardinality bounded) and a hook the events path calls to
// report ingest counts. promhttp renders the exposition format — we never
// hand-format Prometheus text.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds a private registry and the TinyRaven collectors. Use New to
// build one; the zero value is not usable.
type Metrics struct {
	reg *prometheus.Registry

	requests       *prometheus.CounterVec   // {route,method,status}
	duration       *prometheus.HistogramVec // {route}
	pipeRequests   *prometheus.CounterVec   // {pipe,status}
	eventsIngested prometheus.Counter
	eventsQuarantd prometheus.Counter
}

// New builds a Metrics with its own registry and registers all collectors. A
// dedicated registry (not the global default) keeps tests isolated and avoids
// duplicate-registration panics when several instances coexist.
func New() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tinyraven_requests_total",
			Help: "Total HTTP requests by route pattern, method and status.",
		}, []string{"route", "method", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tinyraven_request_duration_seconds",
			Help:    "HTTP request duration in seconds by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		pipeRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tinyraven_pipe_requests_total",
			Help: "Total pipe endpoint requests by pipe name and status.",
		}, []string{"pipe", "status"}),
		eventsIngested: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tinyraven_events_ingested_total",
			Help: "Total events accepted into the ingest buffer.",
		}),
		eventsQuarantd: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tinyraven_events_quarantined_total",
			Help: "Total events routed to a quarantine table.",
		}),
	}
	m.reg.MustRegister(m.requests, m.duration, m.pipeRequests, m.eventsIngested, m.eventsQuarantd)
	return m
}

// Handler serves the metrics exposition format from this instance's registry.
// Mount it at /v0/metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Middleware records request count + duration + status per route. Labels use
// the chi route *pattern* (e.g. "/v0/pipes/{name}.json"), resolved after the
// handler runs since chi fills the RouteContext during ServeHTTP. Pipe routes
// (those with a "name" URL param) also increment the per-pipe counter.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		elapsed := time.Since(start).Seconds()

		route := routePattern(r)
		status := strconv.Itoa(sw.status)
		m.requests.WithLabelValues(route, r.Method, status).Inc()
		m.duration.WithLabelValues(route).Observe(elapsed)

		if name := chi.URLParam(r, "name"); name != "" {
			m.pipeRequests.WithLabelValues(name, status).Inc()
		}
	})
}

// IngestObserved reports ingest outcomes from the events path (orchestrator
// calls it after gatherer.Ingest returns its counts).
func (m *Metrics) IngestObserved(success, quarantined int) {
	if success > 0 {
		m.eventsIngested.Add(float64(success))
	}
	if quarantined > 0 {
		m.eventsQuarantd.Add(float64(quarantined))
	}
}

// routePattern returns the matched chi route pattern, or "unmatched" when no
// route matched (a 404) — both keep the route label low-cardinality.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// statusWriter captures the status code written by the wrapped handler.
//
// ponytail: implements only the http.ResponseWriter surface — it does not
// proxy http.Flusher / http.Hijacker. Fine for the JSON API routes this wraps;
// revisit if a streaming endpoint is ever mounted behind this middleware.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
