# Target ClickHouse 26.3 LTS and build on its feature baseline

TinyRaven targets **ClickHouse 26.3 LTS** (`v26.3.15.4-lts`) as the minimum/default. Docker Compose and CI pin `clickhouse/clickhouse-server:26.3`. 26.6 is the newest stable; LTS is chosen for stability and a known feature floor.

Locking to an LTS lets TinyRaven *rely* on these GA features instead of reimplementing them:

| Feature | TinyRaven uses it for | What we DON'T build |
|---|---|---|
| Query cache (`use_query_cache`, per-query TTL) | Per-pipe result caching | No Redis query-result cache — see below |
| Refreshable MV (`REFRESH EVERY`, GA 24.10) | Materialization pipes that need full recompute | No custom backfill scheduler for those |
| Parameterized queries (`{name:Type}`) | Pipe value params | No string interpolation (ADR 0003) |
| Native JSON type (GA 25.x) | `properties JSON` columns | No custom JSON column handling |
| async_insert (default-on in 26.3) | — (rejected) | Not used — Gatherer batches client-side (ADR 0004) |

## Query caching: ClickHouse query_cache, not Redis

Pipe result caching uses ClickHouse's native `query_cache` (per-pipe opt-in TTL via query settings), not a TinyRaven-built Redis cache. Redis is left to tokens + rate-limiting only. One less cache to build, invalidate, and reason about.

### Per-pipe declaration (amendment)

Caching is **opt-in per pipe and off by default** — silent staleness is a worse surprise than a cache miss for an analytics endpoint. A pipe opts in with a `CACHE_TTL = <seconds>` directive in its ENDPOINT block; when present, TinyRaven appends `SETTINGS use_query_cache = 1, query_cache_ttl = <seconds>` to that pipe's ClickHouse HTTP query. No directive → no caching. TinyRaven writes no cache code; ClickHouse owns the cache.

- **`CACHE_TTL` is a TinyRaven-native field, not Tinybird parity.** Tinybird has no `.pipe` cache-TTL knob (its endpoint caching is internal/automatic), so this is an extension. Consequence: a user who runs *both* `tb` and `tr` against the same files may hit a parse error from `tb` on the unknown directive. Parity of the HTTP API surface is unaffected; only the shared datafile is.
- **Per-param-combo correctness is automatic.** ClickHouse `query_cache` keys on the full query text *including substituted param values*, so each distinct param combination caches independently — no manual cache-key construction. High-cardinality params are bounded by ClickHouse's global `query_cache_max_size` (operator-tuned), which evicts under pressure.
- **Time-based invalidation only.** ClickHouse `query_cache` has no event-driven busting, so a cached pipe may serve results up to `CACHE_TTL` stale after new ingestion. This is acceptable for dashboard-style endpoints and is documented as the trade-off; there is no invalidate-on-ingest in the MVP. Pipes needing fresher-than-TTL semantics either set a low TTL or omit caching (and rely on a materialization, ADR 0010, if precomputation is wanted).

## async_insert: rejected

ClickHouse 26.3 enables async inserts by default, but TinyRaven does not rely on them. async_insert targets many uncoordinated clients doing small inserts; TinyRaven is itself the central batching point (the Gatherer, ADR 0004), where client-side batching is the recommended, lower-overhead path. The Gatherer flush is a batched INSERT, durable once ClickHouse acks it.

## Consequences

- A hard floor of 26.3 features is assumed throughout — running TinyRaven against older ClickHouse is unsupported.
- The next LTS (~Aug 2026) will be evaluated; bumping the floor is a deliberate, tested change.
