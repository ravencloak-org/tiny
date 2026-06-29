package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// readiness caches the combined Redis+ClickHouse probe result for a short TTL so
// a burst of /health/ready calls doesn't hammer the dependencies (ADR 0024).
type readiness struct {
	redis model.Pinger
	ch    model.CHPinger
	ttl   time.Duration

	mu      sync.Mutex
	checked time.Time
	lastErr error
}

func newReadiness(redis model.Pinger, ch model.CHPinger, ttl time.Duration) *readiness {
	return &readiness{redis: redis, ch: ch, ttl: ttl}
}

// check returns nil when both deps are reachable, using a cached result within
// ttl. A nil pinger is treated as "not configured" -> not ready.
func (rd *readiness) check(ctx context.Context) error {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	if !rd.checked.IsZero() && time.Since(rd.checked) < rd.ttl {
		return rd.lastErr
	}
	rd.lastErr = rd.probe(ctx)
	rd.checked = time.Now()
	return rd.lastErr
}

func (rd *readiness) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if rd.redis == nil {
		return errNotConfigured("redis")
	}
	if err := rd.redis.Ping(ctx); err != nil {
		return err
	}
	if rd.ch == nil {
		return errNotConfigured("clickhouse")
	}
	return rd.ch.Ping(ctx)
}

type errNotConfigured string

func (e errNotConfigured) Error() string { return string(e) + " not configured" }

// handleLiveness reports the process is up; zero dependency checks (ADR 0024).
func (s *server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	encodeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadiness gates 200/503 on Redis + ClickHouse reachability (ADR 0024).
func (s *server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if err := s.ready.check(r.Context()); err != nil {
		encodeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"reason": err.Error(),
		})
		return
	}
	encodeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
