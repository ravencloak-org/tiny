# No second analytical/time-series store; read acceleration is materialized views + Redis

TinyRaven does not add TimescaleDB, DuckDB, a search engine, or any second store to "speed up" or "cache" reads in front of ClickHouse. Read acceleration has exactly two layers, both already in the stack:

1. **Materialized views** — pre-aggregate on ingest. Append-only event data rolls forward incrementally into small target tables; dashboard queries hit the rollup, not the raw rows. Declared in `.pipe` files (`MATERIALIZATION` / `TARGET_TABLE`).
2. **Redis result cache** — repeated identical pipe queries served from Redis with a per-pipe `CACHE_TTL`, skipping ClickHouse entirely.

Ladder: **MV (pre-aggregate) → Redis (result TTL) → ClickHouse (raw)**.

## Why

- **ClickHouse is already the fast append-only time-series store.** Columnar OLAP built for `GROUP BY toStartOfHour(...)` over large immutable scans. TimescaleDB is Postgres-with-hypertables — row-oriented at heart; on the dashboard aggregation shape it loses to ClickHouse in public benchmarks. Putting Timescale "in front" of CH caches a fast store behind a slower one.
- **The append-only-rollup job is what materialized views are for.** Data that "keeps appending like a WAL and never changes" is the ideal MV input — each new part rolls forward automatically. That is the cache; it needs no second database.
- **A second store doubles the data and adds a sync pipeline.** CH↔Timescale replication means two sources of truth and lag — dashboards would show stale/inconsistent numbers. A result cache (Redis, TTL-bounded) has bounded, understood staleness; a mirrored DB does not.
- **No PostgreSQL (ADR 0001).** Timescale is a Postgres extension — it drags a whole Postgres into every deploy path, undercutting the minimal-infra positioning.
- **Parity.** Tinybird is ClickHouse-only (its speed-up primitive is materialized pipes, not a second engine). Matching that keeps the drop-in thesis intact.

## Consequences

- A slow pipe is fixed by adding a materialized view or a `CACHE_TTL`, never by introducing a new backend.
- The only stateful services remain ClickHouse (data) and Redis (metadata + hot cache) — see ADR 0001.

## Considered and rejected

- **TimescaleDB as a read cache / mirror** — slower than CH for these queries, adds Postgres (violates ADR 0001), introduces a sync pipeline and a second source of truth for zero read-speed gain.
- **DuckDB / embedded OLAP as a query-side accelerator** — no workload here that CH + MV + Redis doesn't already cover; adds a second query engine to reason about.
