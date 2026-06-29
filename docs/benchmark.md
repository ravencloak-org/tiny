# Ingestion benchmark

TinyRaven's Phase 5 success criterion (`MILESTONE.md`) is **≥ 10,000 events/second
on a single t3.large**. This doc covers how throughput is measured, how to
reproduce it, and the reference figures.

## What's measured

The load generator (`scripts/loadtest/`) drives the ingestion path TinyRaven
owns end to end:

```
POST /v0/events  ->  auth + rate limit  ->  parse NDJSON  ->  Gatherer (validate,
buffer, ack)  ->  batched native insert into ClickHouse
```

A `202 Accepted` is returned on **buffer**, not on the ClickHouse write
(ack-on-buffer, ADR 0004). The Gatherer then flushes batches to ClickHouse on
`max(N events, 5s)`. So the reported **events/s** is request-acked throughput;
the `successful_rows` / `quarantined_rows` counts come from the server's 202
body. Latency percentiles are per-HTTP-request round trips.

## Methodology

- **Workload:** N concurrent workers, each in a tight loop building a batch of
  `-batch` NDJSON rows and POSTing it, for `-duration`. Connections are kept
  alive (pooled) so the benchmark measures ingestion, not TCP/TLS dialing.
- **Row shape:** matches `examples/quickstart/events.datasource`
  (`user_id`, `event`, `timestamp` + an `event_id`), so rows insert cleanly
  rather than landing in quarantine.
- **Throughput** = total events in 202'd requests ÷ wall-clock duration.
- **Percentiles** = nearest-rank over every request's latency.

## Running it

Start TinyRaven + ClickHouse + Redis (e.g. `docker compose up`), make sure the
target datasource is deployed (`tr deploy`), then:

```bash
go run ./scripts/loadtest \
  -url http://localhost:8000 \
  -token "$TR_ADMIN_TOKEN" \
  -datasource events \
  -workers 50 \
  -duration 30s \
  -batch 1000
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-url` | `http://localhost:8000` | TinyRaven base URL |
| `-token` | (empty) | bearer token (`TR_ADMIN_TOKEN` or a write-scoped token) |
| `-datasource` | `events` | target datasource (`?name=`) |
| `-workers` | `50` | concurrent workers |
| `-duration` | `30s` | run length |
| `-batch` | `1000` | events per request |
| `-timeout` | `30s` | per-request HTTP timeout |

Sample output:

```
--- results ---
elapsed:        30.0s
requests:       ok=5310 err=0
events sent:    5310000
server rows:    successful=5310000 quarantined=0
throughput:     177000 events/s
latency p50:    18.2ms
latency p95:    71ms
latency p99:    104ms
```

The generator exits non-zero if any request failed, so it doubles as a CI smoke
check.

## Reference figures

| Environment | Config | Throughput | p95 | Notes |
|-------------|--------|-----------|-----|-------|
| **Target** | single t3.large | **≥ 10k events/s** | — | Phase 5 success criterion |
| **Measured (local)** | 50 clients, batch 1000, single node | **~177k events/s** | **~71 ms** | ack + persisted to ClickHouse |

> **The measured figure is local, on Apple Silicon (a developer laptop), single
> node — not a t3.large.** It is not a substitute for the t3.large target; it
> shows the Go ingestion path is far from the bottleneck at this scale. Treat the
> t3.large number as the official bar and re-run on that instance to certify it.

## Tuning notes

- **Batch size dominates.** Few large requests beat many tiny ones — per-request
  overhead (auth, rate-limit, parse) amortises across the batch. Start at
  `-batch 1000` and raise it before adding workers.
- **Workers past the core count** mostly add latency, not throughput, once the
  Gatherer and ClickHouse insert are saturated.
- **Gzip** the body (`Content-Encoding: gzip`, ADR 0023) when the bottleneck is
  the network rather than CPU; it trades client CPU for bytes on the wire.

## Query latency

Ingestion is half the story; the other half is **how fast published pipes
answer**, which is TinyRaven's whole point. The query generator
(`scripts/querybench/`) drives the read path TinyRaven owns:

```
GET /v0/pipes/<pipe>.json?<params>  ->  auth  ->  template substitution +
param validation/escaping  ->  ClickHouse HTTP (FORMAT JSON)  ->  proxied back
```

TinyRaven does **no per-row work** on this path: it substitutes `{{Type(name)}}`
params into the pipe SQL, escapes them, and proxies the query to ClickHouse. So
the gap between client latency and ClickHouse's own `statistics.elapsed` is the
TinyRaven overhead — template substitution plus the HTTP proxy hop — and nothing
more.

### What's measured

Each request records two latencies:

- **Client (wall) latency** — the full HTTP round trip the caller sees, reported
  as p50/p95/p99 (nearest-rank, same definition as the ingestion benchmark).
- **CH elapsed** — ClickHouse's server-side query time, parsed from the response
  body's `statistics.elapsed` (seconds, in the `FORMAT JSON` envelope), reported
  as p50/p95 when present. This isolates the database from the proxy.

Throughput is total requests ÷ wall-clock duration. The generator exits non-zero
if any request fails a 2xx, so it doubles as a CI smoke check.

### Cached vs uncached: the `-distinct` knob

Pipes opt into ClickHouse's `query_cache` by declaring `CACHE_TTL <seconds>` in
the `ENDPOINT` block (**ADR 0009**). Once a pipe is cached, a repeated query with
**identical params** is served from the cache and comes back sub-millisecond —
ClickHouse never re-executes it. `-distinct` controls how often params repeat,
which is how we separate the two regimes:

| `-distinct` | Param values | Regime | What it measures |
|-------------|--------------|--------|------------------|
| `1` | all identical (`user_id=u`) | **cached** | best case — query_cache hits, proxy + cache lookup only |
| large (e.g. `100000`) | rarely repeat (`user_id=u0`, `u1`, …) | **uncached** | worst case — ClickHouse re-executes every query |

Run `-distinct 1` to certify the cache-hit fast path, then a large `-distinct` to
measure cold query execution. The truth for a real workload sits between the two,
weighted by your cache hit rate.

### Running it

Start TinyRaven + ClickHouse + Redis, deploy a pipe with `CACHE_TTL` set
(`tr deploy`), then:

```bash
# cached: one param value, max query_cache hits
go run ./scripts/querybench \
  -url http://localhost:8010 \
  -token "$TR_READ_TOKEN" \
  -pipe user_metrics \
  -params user_id=u \
  -workers 50 \
  -duration 15s \
  -distinct 1

# uncached: rotate through many param values, mostly cache misses
go run ./scripts/querybench ... -distinct 100000
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-url` | `http://localhost:8010` | TinyRaven base URL |
| `-token` | (empty) | bearer token (read-scoped, or `TR_ADMIN_TOKEN`) |
| `-pipe` | `user_metrics` | pipe endpoint (served at `/v0/pipes/<pipe>.json`) |
| `-params` | `user_id=u` | rotating param as `key=value`; the value gets a `0..distinct-1` suffix |
| `-workers` | `50` | concurrent workers |
| `-duration` | `15s` | run length |
| `-distinct` | `1` | distinct param-value combos to rotate (1 = all-same → max cache hits) |
| `-timeout` | `30s` | per-request HTTP timeout |

> The example pipe (`examples/quickstart/user_metrics.pipe`) takes a single
> `{{String(user_id)}}`, so `-params` is one `key=value` pair: the value is the
> rotation base and querybench appends the combo index. For `-distinct 1` the
> value is sent verbatim (every request is `user_id=u`).

Sample output:

```
querybench: 50 workers -> http://localhost:8010/v0/pipes/user_metrics.json [cached (max cache hits), distinct=1] for 15s

--- results ---
elapsed:               15.0s
requests:              ok=... err=0
throughput:            ... queries/s

latency                p50          p95          p99
  client (wall):       ...ms        ...ms        ...ms
  CH elapsed:          ...ms        ...ms        -
```

### Reference figures

> **Placeholder — to be filled after a real run on the reference box.** Run the
> two scenarios above and paste the numbers in. The point of the table is the
> contrast: cached p50 should be well under a millisecond of ClickHouse time
> (query_cache hit), while uncached reflects actual query execution cost.

Measured locally (Apple Silicon, single node — `tr` + ClickHouse 26.3 + Redis all
on one laptop, so high-concurrency rows are CPU-contended; 100k-row `events`,
point query on the sorting key):

| Scenario | Queries/s | Client p50 | Client p95 | Client p99 | CH elapsed p50 |
|----------|-----------|-----------|-----------|-----------|----------------|
| **Single-flight** (`-workers 1`) | 479 | **1.8ms** | 3.7ms | 7.2ms | 0.35ms |
| **8 workers** (`-distinct 1`) | 2,417 | **3.0ms** | 5.3ms | 9.2ms | 0.50ms |
| **50 workers cached** (`-distinct 1`) | 2,821 | 14.6ms | 35.5ms | 62.5ms | 3.5ms |
| **50 workers uncached** (`-distinct 1000`) | 2,316 | 19.0ms | 45.2ms | 65.3ms | 7.9ms |

**Headline:** per-query latency is **~1.8ms p50** single-flight — TinyRaven adds
only ~1.4ms over ClickHouse's own ~0.35ms (template substitution + pooled HTTP
proxy, no per-row work). At 50 workers the laptop is CPU-saturated (everything on
one box), so those rows reflect contention, not the proxy.

> **Connection pooling matters:** the CH HTTP client uses a tuned transport
> (`MaxIdleConnsPerHost: 512`). With the Go default (2), 50 concurrent queries
> thrashed connections → p50 92ms + thousands of errors. Pooling cut that to
> 19ms p50 with ~0 errors (4–5×). Caching helps less here only because a 100k-row
> point query is already sub-millisecond; on heavy aggregations `CACHE_TTL` is the
> bigger lever.

### Reading the results

- **Client p50 − CH elapsed p50 ≈ TinyRaven overhead.** Because the read path is
  pure template-substitution + proxy, this gap should be small and roughly flat
  across cached and uncached runs. If it grows under load, the bottleneck is the
  Go proxy / connection pool, not ClickHouse.
- **Cached CH elapsed near zero** confirms `CACHE_TTL` (ADR 0009) is doing its
  job — query_cache hits don't re-execute, so server-side time collapses and the
  client latency is dominated by the network round trip.
- **Workers past the core count** mostly add client-side latency once ClickHouse
  (uncached) or the proxy (cached) is saturated — raise `-workers` until
  queries/s plateaus, then back off.
