# Materialized pipes: incremental MV by default, refreshable MV when declared

A `.pipe` with `TYPE materialization` maps to one of two ClickHouse mechanisms, chosen per pipe:

- **Incremental MV (default).** Real-time — processes each insert into the source. Because a ClickHouse incremental MV only sees rows inserted *after* it is created, `tr deploy` of a new incremental MV **backfills** the target (`INSERT INTO target SELECT ... FROM source`) over existing data. Backfill is **on by default** (correctness — an un-backfilled MV silently returns partial aggregates), prints a row-count warning before running on large sources, and can be skipped with `--no-backfill`.
- **Refreshable MV.** If the `.pipe` declares a `REFRESH EVERY ...`, the pipe maps to a ClickHouse refreshable MV (GA 24.10) — periodic full recompute, atomically swapped. No backfill needed; freshness lags by the refresh interval.

## Why

- Incremental fits real-time aggregates; refreshable fits complex/expensive aggregates where periodic full recompute is simpler and freshness can lag. ClickHouse provides both — TinyRaven exposes the choice rather than forcing one.
- Backfill-by-default closes the silent-partial-aggregate trap that catches everyone deploying a fresh incremental MV.

## Consequences

- Backfill on a huge source is expensive; the size warning + `--no-backfill` give the user the escape hatch.
- Possible small double-count window between MV-create and backfill on the incremental path. MVP may document the overlap; exact partition-fencing is a later refinement.
