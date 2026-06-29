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
