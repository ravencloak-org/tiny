# Health: split liveness/readiness, readiness gates traffic and flips on drain

Two endpoints, deliberately different jobs:

- **`/health` (liveness)** — `200` if the process is up. **Zero dependency checks.** A liveness probe must only answer "is this process wedged?"; gating it on Redis/ClickHouse would make an orchestrator kill a healthy `tr` whenever a dependency blips, turning a recoverable outage into a crash loop.
- **`/health/ready` (readiness)** — `200` when the node can serve traffic, `503` otherwise. Gated on **both** Redis **and** ClickHouse: Redis `PING` + ClickHouse `SELECT 1`. Either down → `503`, because TinyRaven can do nothing useful without both (Redis holds token/metadata, ClickHouse serves queries + ingest). The check result is **cached ~2–3s** so probe storms from load balancers don't hammer the dependencies.

**Graceful drain (SIGTERM):** readiness **flips to `503` immediately** while liveness **stays `200`**. The load balancer / k8s stops routing new traffic to the draining node; the orchestrator does not kill it mid-drain. The node then finishes in-flight requests, flushes the Gatherer batch (ADR 0004), and exits 0. Without this flip, ADR 0004's "graceful drain, no loss" does not hold behind a load balancer — the LB keeps sending traffic to a pod that is shutting down. Implemented as a `shuttingDown` flag the readiness handler checks first, before the cached dependency result.

## Considered Options

- **Single `/health` gating on dependencies** — rejected: a dependency blip would fail liveness and trigger a kill/restart loop instead of a recover.
- **Readiness ignores shutdown state** — rejected: drops requests during rolling deploys behind an LB.
- **Degrade instead of 503 when one dep is down** — rejected for now: with no path that works on Redis-only or ClickHouse-only, "partially ready" has no useful meaning; revisit only if a read-only-from-cache mode is ever added.

## Consequences

- Liveness never touches the network; it cannot flap.
- The ~2–3s readiness cache means a dependency recovering takes up to one cache window to be reflected — acceptable for probe cadence.
- The `shuttingDown` flag couples health to the shutdown sequence in ADR 0004; they must be wired together.
- chi only routes these endpoints — it has no health/readiness/graceful-shutdown support. The drain is stdlib `http.Server.Shutdown(ctx)` + `signal.NotifyContext` (SIGTERM); the readiness flip, dependency probes, and caching are our own handler code.
