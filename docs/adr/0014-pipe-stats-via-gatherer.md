# pipe_stats observability recorded through the Gatherer

Every pipe/SQL query records a row in `tinybird.pipe_stats` (latency, rows read, bytes, error, token). We feed these rows through the **same Gatherer** used for `/v0/events` ingestion — treating `pipe_stats` as an internal datasource. After the query handler writes its response, it drops a stat struct on the Gatherer channel and returns; the Gatherer batches and flushes on `max(N, 5s)` like any other datasource.

## Considered Options

- **Synchronous INSERT per query** — rejected. Adds latency to every query response and floods ClickHouse with one tiny part per query, creating exactly the merge pressure the Gatherer exists to avoid.
- **Dedicated stats-writer goroutine** — rejected. A second batching path that duplicates what the Gatherer already does, for no benefit.
- **Gatherer-fed internal datasource** — chosen. Zero added query latency, no tiny-parts problem, reuses code already built for ingestion.

## Consequences

- Stat recording is **best-effort**: a crash drops in-flight stats still in the buffer. Same trade as ingestion ack-on-buffer (ADR 0004), and acceptable — losing a few observability rows is not losing user events.
- `pipe_stats` shares the Gatherer's backpressure/flush behavior; no separate tuning path.
