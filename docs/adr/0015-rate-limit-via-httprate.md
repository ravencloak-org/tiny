# Rate limiting via go-chi/httprate, in-memory

We rate-limit API requests using `github.com/go-chi/httprate` (same maintainers as our `chi` router) rather than hand-rolling a Redis counter. It is a sliding-window counter with a custom key function (per-token), a custom 429 handler (wired to our error envelope, ADR 0012), and built-in `X-RateLimit-Limit/Remaining/Reset` + `Retry-After` headers — Tinybird-shape. The limit is configurable; default generous, `0`/unset disables it (self-hosters own their box).

## Considered Options

- **Hand-rolled fixed-window in Redis** (`INCR` + `EXPIRE`) — rejected. Correct but it's code we own, and fixed-window allows a 2× burst at the window boundary.
- **`go-chi/httprate`, in-memory** — chosen. Sliding-window (no boundary burst), per-token keying, 429 + rate headers, all out of the box. ~15 lines of wiring, zero algorithm code.

## Consequences

- **In-memory, not Redis** — surprising given Redis is our hot store, but correct for a single-binary single-node deployment: no network round-trip on the hot path, counters are process-local. `httprate-redis` is a drop-in backend swap *if* multi-node HA is ever in scope (currently deferred).
- Counters reset on process restart — acceptable; the limit is a safety valve, not a billing meter.
- Scope is per-token now; key function stays extensible toward `token+pipe` without an algorithm change.
