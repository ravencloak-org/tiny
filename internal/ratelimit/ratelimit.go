// Package ratelimit provides per-token request rate limiting for the API.
// It wraps github.com/go-chi/httprate (same maintainers as chi) — a sliding
// window counter, in-memory, keyed by the authenticated token (ADR 0015).
// The 429 response uses the shared Tinybird error envelope (ADR 0012).
package ratelimit

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
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

// PerPipe returns a chi-compatible middleware for /v0/pipes/{name}.json that
// limits each (token, pipe) pair independently. The effective limit is the
// pipe's own RATE_LIMIT when limitFor(pipe) > 0, otherwise defaultRPS. An
// effective limit <= 0 means unlimited (pass-through), so a 0 default combined
// with a 0 (or unset) pipe limit disables limiting for that pipe — matching
// PerToken's "0 disables" rule (ADR 0015), but resolved per request so one
// pipe can opt in to a limit while the rest stay open.
//
// Mount it on the group that has already matched the {name} route param;
// chi.URLParam(r, "name") supplies the pipe name. The composite key is
// "<token>:<pipe>", so each pair gets its own sliding window.
//
// ponytail: httprate's middleware bakes the request limit in at construction,
// so a per-pipe override needs one limiter per distinct effective-rps. We keep
// a lazily-populated map[rps]*httprate.RateLimiter (guarded by a mutex) and
// drive the chosen limiter via its OnLimit(w, r, key) primitive with our own
// composite key — each limiter owns its own counter, and within a limiter the
// composite key isolates pipes/tokens. The map is bounded by the number of
// distinct RATE_LIMIT values in use (small).
func PerPipe(defaultRPS int, limitFor func(pipe string) int, opts ...Option) func(http.Handler) http.Handler {
	c := &config{window: defaultWindow, keyFn: tokenKey}
	for _, o := range opts {
		o(c)
	}
	if limitFor == nil {
		limitFor = func(string) int { return 0 }
	}

	var mu sync.Mutex
	limiters := make(map[int]*httprate.RateLimiter)
	limiterFor := func(rps int) *httprate.RateLimiter {
		mu.Lock()
		defer mu.Unlock()
		if l, ok := limiters[rps]; ok {
			return l
		}
		l := httprate.NewRateLimiter(rps, c.window)
		limiters[rps] = l
		return l
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pipe := chi.URLParam(r, "name")

			eff := defaultRPS
			if pl := limitFor(pipe); pl > 0 {
				eff = pl
			}
			if eff <= 0 { // unlimited for this pipe
				next.ServeHTTP(w, r)
				return
			}

			key, err := c.keyFn(r)
			if err != nil {
				// Can't identify the caller: bound all such requests together
				// rather than letting them bypass the limit.
				key = "err"
			}

			if limiterFor(eff).OnLimit(w, r, key+":"+pipe) {
				apierr.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
