# /v0/events: per-row validation in Go, bad rows quarantined (not batch-rejected)

`POST /v0/events?name={datasource}` accepts NDJSON (line-by-line) or a single JSON object. Each row is **validated in Go against the declared datasource schema before buffering**: good rows go to the Gatherer, rows that fail validation are written to a `{datasource}_quarantine` ClickHouse table (fed through the same Gatherer, like `pipe_stats`) carrying the raw line, the validation error(s), and insert time. The request still returns `202` with `{"successful_rows": N, "quarantined_rows": M}` — matching Tinybird's quarantine behavior. `name=` is required (`400` if missing); body over a configurable max (default 10 MB) is `413`.

## Considered Options

- **Reject the whole batch on any bad row** — rejected. Breaks drop-in Tinybird clients, which expect quarantine + a 202.
- **Let ClickHouse reject bad rows on INSERT** — rejected, and it's the key constraint: `clickhouse-go` batch insert writes a block atomically, so one bad row fails the entire block. Selective quarantine is impossible after handoff to the driver — validation **must** happen in Go, pre-buffer, to split good from bad.
- **Validate in Go + quarantine table** — chosen. Small code (NDJSON scan + a type-check loop), quarantine reuses the Gatherer.

## Consequences

- Validation logic in Go must track CH type semantics closely enough to predict what the driver would accept; drift means a row passes Go but fails CH (falls back to the block-level failure). Keep the type-check aligned with the declared schema.
- Quarantine tables are real ClickHouse tables per datasource — visible, queryable, parity with Tinybird.
