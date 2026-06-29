// Package ratelimit provides per-token request rate limiting for the API.
// It wraps github.com/go-chi/httprate (same maintainers as chi) — a sliding
// window counter, in-memory, keyed by the authenticated token (ADR 0015).
// The 429 response uses the shared Tinybird error envelope (ADR 0012).
package ratelimit

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"

	"github.com/tinyraven/tinyraven/internal/apierr"
)

// defaultWindow is the sliding-window length; defaultRPS requests are allowed
// per window. One second gives a requests/second limit.
const defaultWindow = time.Second

// KeyFn extracts the rate-limit key from a request. It matches httprate's key
// function shape so the orchestrator can supply its own (e.g. keying on the
// token it stored in the request context) without this package importing
// internal/api.
type KeyFn func(r *http.Request) (string, error)

// Option configures the middleware.
type Option func(*config)

type config struct {
	window time.Duration
	keyFn  KeyFn
}

// WithWindow sets the sliding-window length (default 1s).
func WithWindow(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.window = d
		}
	}
}

// WithKeyFn overrides how the limiter keys requests (default: bearer token from
// the Authorization header, falling back to ?token=, then the remote address).
func WithKeyFn(fn KeyFn) Option {
	return func(c *config) {
		if fn != nil {
			c.keyFn = fn
		}
	}
}

// PerToken returns a chi-compatible middleware limiting each token to
// defaultRPS requests per window. defaultRPS <= 0 disables limiting entirely
// (self-hosters own their box; ADR 0015 — "0/unset disables it").
//
// ponytail: in-memory + global default only. Per-pipe RATE_LIMIT overrides
// (the orchestrator wires the pipe's limit later) and a multi-node
// httprate-redis backend are the upgrade path (ADR 0015 / 0031).
func PerToken(defaultRPS int, opts ...Option) func(http.Handler) http.Handler {
	if defaultRPS <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	c := &config{window: defaultWindow, keyFn: tokenKey}
	for _, o := range opts {
		o(c)
	}
	return httprate.Limit(
		defaultRPS,
		c.window,
		httprate.WithKeyFuncs(httprate.KeyFunc(c.keyFn)),
		httprate.WithLimitHandler(func(w http.ResponseWriter, _ *http.Request) {
			apierr.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
		}),
	)
}

// tokenKey keys on the authenticated bearer token without depending on
// internal/api's context key: Authorization header first, then ?token=, then
// the remote address so an unauthenticated burst is still bounded.
func tokenKey(r *http.Request) (string, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		return "tok:" + strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")), nil
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return "tok:" + t, nil
	}
	return "ip:" + r.RemoteAddr, nil
}
