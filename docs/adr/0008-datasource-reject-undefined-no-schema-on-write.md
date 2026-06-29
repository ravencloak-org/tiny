# Datasources must be declared; ingestion to an undefined datasource is rejected

A datasource must be declared in a `.datasource` file and applied via `tr deploy` before it can receive events. `POST /v0/events` to an undefined datasource is rejected (400/404). There is no schema-on-write auto-creation in the MVP.

When `.datasource` omits `ENGINE`, the default is `MergeTree` with `ORDER BY tuple()`. All `ENGINE_*` directives (sorting key, partition key, TTL) are forwarded to ClickHouse verbatim — TinyRaven parses, it does not reinterpret.

## Why

- **Git is the source of truth for schema** (see ADR 0001) and `tr deploy` owns the diff/migration model (ADR 0006). Schema-on-write would create tables behind both, from event shape, bypassing review and migration classification.
- Forwarding `ENGINE_*` verbatim keeps full ClickHouse engine expressiveness without TinyRaven needing to model it.

## Consequences

- Loses Tinybird's "just POST events and a datasource appears" onboarding. Documented parity gap, traded for a coherent git/deploy model.
- An opt-in `--auto-schema` (infer + register a `.datasource` from the first event) may be added later for onboarding parity; it is not MVP.
