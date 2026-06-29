# Ingestion is ack-on-buffer with 202 Accepted; durable WAL deferred

`/v0/events` acknowledges the client as soon as events are accepted into the in-memory Gatherer buffer, returning **202 Accepted** (not 200/201) — the honest signal for "buffered, not yet durably stored." The Gatherer flushes to ClickHouse on `max(N events, 5s)`. On SIGTERM the buffer is drained (graceful flush) before exit.

## Delivery contract

- **Normal restart / deploy (SIGTERM):** no loss — graceful drain flushes the buffer first.
- **Hard crash (OOM, kill -9, node failure):** up to one batch window (N events / 5s) of acked-but-unflushed events is lost. At-most-once under hard failure.
- **Phase 2/3:** optional on-disk WAL (append before ack, flush async, truncate on success) upgrades this to at-least-once for users who need it.

## Why

- Matches Tinybird's fire-and-forget events model and is what makes the 10k events/s target reachable — ack-on-flush would couple client latency to batch timing and crater throughput.
- Graceful drain covers the common failure mode (restarts/deploys), so the residual loss window only applies to genuine hard crashes — acceptable for MVP, closed later by the WAL.

## Not ClickHouse async_insert

The Gatherer is client-side batching. TinyRaven does **not** rely on ClickHouse `async_insert` (default-on in 26.3) — that feature targets many uncoordinated clients doing small inserts, whereas TinyRaven is itself the central batching point, where client-side batching is recommended and lower-overhead. The Gatherer flush is a single batched INSERT, durable once ClickHouse acks it. See ADR 0009.

## Consequences

- 202, not 201 — clients must not treat the ack as a durability guarantee.
- A bounded data-loss window exists until the WAL ships; documented, not silent.
