# Changelog

All notable changes to TinyRaven are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-06-29

First release: a working, self-hosted, Tinybird-API-compatible analytics backend
over OSS ClickHouse. Covers MILESTONE Phases 1 (MVP ingestion + query) and 2 (API
publishing + deployment). Single `tr` binary = server + CLI.

### Added — Phase 1 (MVP: core ingestion + query)
- HTTP server on `net/http` + `chi` router.
- `POST /v0/events` — JSON / NDJSON ingestion; per-row validate + quarantine
  (ADR 0018); `202 {successful_rows, quarantined_rows}` ack-on-buffer (ADR 0004).
- `GET /v0/pipes/{name}.json` — parameterized SQL pipes via `{{Type(name,default)}}`
  → ClickHouse `{name:Type}` parameters, injection-proof (ADR 0003).
- `GET /health` (liveness) + `GET /health/ready` (Redis + ClickHouse, cached) — ADR 0024.
- Gatherer: in-process batching, flush on `max(10,000 events, 5s)`, graceful drain.
- `.datasource` parser (SCHEMA/ENGINE/ENGINE_* + structural validation, ADR 0008/0027)
  → Redis schema registry (ADR 0001 — Redis-only metadata, no Postgres).
- `.pipe` parser + in-memory pipe registry with atomic hot-reload swap (ADR 0020).
- Bearer token auth middleware → Redis token store (ADR 0005).
- `tr local start` → Docker Compose (ClickHouse 26.3 LTS + Redis AOF + TinyRaven).
- Dev hot reload on `.datasource`/`.pipe` change (mtime poll).

### Added — Phase 2 (API publishing + deployment)
- `tr deploy` — validate all files, diff schema against live `system.columns`,
  create missing tables, apply additive `ALTER TABLE ADD COLUMN`; breaking changes
  detected and refused (shadow-table/`EXCHANGE` deferred to Phase 3, ADR 0007).
- Full pipe param types: `String`, `DateTime`, `Int64`, `Float64`, `UUID`,
  `Boolean` (+ `Int32`, `Date`, `DateTime64`) — validated + normalized, 400 on bad input.
- `GET /v0/sql` — read-only ClickHouse SQL proxy (`readonly=2` + caps, ADR 0011).
- `GET /v0/metrics` — Prometheus exposition via `prometheus/client_golang`.
- `GET /v0/openapi.json` — runtime OpenAPI 3.0 spec from the pipe registry (ADR 0017).
- Per-token rate limiting — in-memory `httprate` sliding window (ADR 0015).
- Query observability — `pipe_stats` table fed by a non-blocking background
  flusher (ADR 0014).
- Tinybird-compatible error envelope `{"error": "..."}` + status mapping +
  `X-DB-Exception-Code` passthrough (ADR 0012).

### Infrastructure
- CI (GitHub Actions): gofmt + `go vet`, race tests with coverage, integration
  tests against ClickHouse + Redis service containers, build; Codecov upload.
- CodeRabbit review config; Dockerfile (static `tr` binary).

### Known limitations (deliberate, see `// ponytail:` notes)
- Single-node `tr` (ADR 0031); ack-on-buffer is at-most-once on hard crash until WAL.
- `pipe_stats` lives in the workspace DB (not `tinybird.pipe_stats`); drops on overflow.
- Rate limit is a global per-token default; per-pipe `RATE_LIMIT` wiring pending.
- `/v0/sql` uses the `readonly=2` setting; a dedicated read-only CH user is the
  production upgrade.

[0.1.0]: https://github.com/ravencloak-org/tiny/releases/tag/v0.1.0
