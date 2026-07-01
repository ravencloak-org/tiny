# Changelog

All notable changes to TinyRaven are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.6] — 2026-07-01

### Added
- CORS on the API (`Access-Control-Allow-Origin: *` + preflight short-circuit, `internal/api/middleware.go`) so browser dashboards can read pipe endpoints cross-origin — Tinybird parity (pipes are CORS-open for public read tokens; ADR 0025). Auth unchanged; `*` grants read visibility, not access.
- `/use-cases` now renders a **live dashboard**: two visx charts (top pages, views over time) that fetch the demo pipes from prod in-browser on load. Replaces the static JSON receipt.

## [0.3.5] — 2026-07-01

### Added
- Live demo links on `/use-cases`: clickable prod endpoints (`tiny-api.ravencloak.org`) backed by a read-only demo token (scope `READ:top_pages`, `READ:views_over_time` — 403 on anything else), returning real JSON from 300 seeded events.
- Dashboards demo baked into the API image (`/project`) so its pipes survive container redeploys; table + data persist in the ClickHouse volume, token in Redis AOF.

## [0.3.4] — 2026-07-01

### Fixed
- Marketing site nginx now serves Next static-export subroutes: `try_files $uri $uri.html` returns `/use-cases.html` for `/use-cases` instead of a dir 301 → 403 (the redirect also dropped https→http behind Cloudflare). Home page was unaffected.

## [0.3.3] — 2026-07-01

### Added
- Marketing site: `/use-cases` page mapping TinyRaven to Tinybird's eight documented use cases with honest coverage badges (Live / Via ClickHouse / Partial) and a demonstrated user-facing-dashboards spotlight using real demo output.
- `examples/dashboards-demo/`: reproducible drop-in demo (Tinybird dashboards use case) — `.datasource`/`.pipe` files, `run.sh` (Apple `container` local runtime), sample events, and verified receipts.

### Fixed
- Nav section links now resolve from any route (`/#features` form) and expose the new Use cases page.

### Known issues
- `tr deploy` cannot bootstrap a missing target database (native client pins the session to it → `UNKNOWN_DATABASE`); pre-create the DB. See #71. Prod compose is unaffected (ClickHouse image creates `tr_main`).

## [0.3.2] — 2026-06-30

### Changed
- Auto-deploy on release is now **version-tagged via the Dokploy API** (`compose.update` env → `compose.deploy`) instead of a webhook on `:latest`. A version tag is a new image reference, so Dokploy pulls the new digest every time; the `:latest` webhook recreated from cache and silently never updated.

## [0.3.1] — 2026-06-30

### Added
- API read endpoints toward full Tinybird `/v0` parity: `GET /v0/datasources`, `GET /v0/datasources/{name}`, `GET /v0/pipes`, `GET /v0/pipes/{name}` (all ADMIN), plus pipe output formats `GET /v0/pipes/{name}.{csv,ndjson}` (READ-scoped) alongside the existing `.json`.
- Distribution: signed APT + RPM repos published to GitHub Pages (`apt-get install tinyraven`); GoReleaser Scoop/AUR/Nix/winget publisher blocks (fail-safe — push only when their token exists); install matrix + `docs/install.md`.
- `docs/parity-gaps.md` ranked `/v0` parity audit.

### Changed
- Prod deploy model: image tags pinned to `latest`; release builds `:version` + `:latest` then triggers the Dokploy deploy webhook (`pull_policy: always` recreates on the new digest). Dokploy panel env is the documented source of truth.
- APT/RPM gh-pages publish now does a scoped `rsync --delete` of `apt/`+`rpm/` (keep-2 pool prune actually removes old blobs); `helm-publish` + `apt-publish` share a concurrency group to avoid gh-pages races.

### Tests
- `internal/apierr` and `internal/model` 0→100%; `internal/pipe` 86.7→96% (error-envelope mapping, `Token.HasScope` auth gate, registry hot-reload swap, control-flow branch + non-boolean-condition rejection).

### Security
- Rotated the admin token (purged the old value from the Dokploy env and Redis `tr:token:`).

## [0.3.0] — 2026-06-30

### Added
- Control-flow pipe templating: `{% if/elif/else/end %}` + `defined()` via expr-lang (ADR 0003).
- Resource-token materialization: `TOKEN "x" READ/APPEND` in files → scoped tokens on `tr deploy` (ADR 0030).
- `tr deploy --check` (dry run), `tr login`, `tr status`.
- Per-pipe `RATE_LIMIT` enforcement (token+pipe; ADR 0015); `/tr/v1/docs` UI (off by default, `TR_DOCS_ENABLED`; ADR 0017); dedicated read-only ClickHouse user (ADR 0011).
- Release signing (GPG) + Helm chart published to GitHub Pages.

### Changed
- ponytail cuts: strconv.Itoa over hand-rolled, merged CHPinger into Pinger, dropped dead Connector field.

## [0.2.0] — 2026-06-29

Phases 3–5 + scoped auth + a query-path perf fix.

### Added
- **Branching** (Phase 3): `tr_{branch}` ClickHouse DB per git branch; `tr deploy --branch` / `tr local start --branch`.
- **Materialized views** + **breaking migrations** (shadow → backfill → `EXCHANGE TABLES`, `--allow-breaking`).
- **Config**: `~/.tinyraven/config.yml` (Tinybird-compatible) + `TINYBIRD_HOST/TOKEN/WORKSPACE` env.
- **Connectors** (Phase 5): Kafka/S3/PostgreSQL as ClickHouse-native engines in `.datasource` (ADR 0019) + templates; BI-tool docs; load + query benchmarks (`scripts/loadtest`, `scripts/querybench`).
- **Distribution** (Phase 4): GoReleaser (binaries/deb/rpm), Helm chart, Heroku/Railway/DO/AWS-CFN manifests, deploy docs.
- **Scoped tokens**: `tr token create/ls/rm`; per-scope enforcement — `READ:<pipe>`, `APPEND:<ds>`, `ADMIN`, wildcards (ADR 0005). Frontends can hold a read-only pipe token.
- `TR_PIPE_RATE_LIMIT` (configurable per-token rate limit; 0 disables).

### Changed / Fixed
- **perf:** tuned ClickHouse HTTP transport (`MaxIdleConnsPerHost: 512`) — default (2) thrashed connections under load (p50 92ms + errors); pooled = ~19ms p50, ~0 errors (4–5×). Query latency now CH-bound: **single-flight p50 ~1.8ms** (CH ~0.35ms).

## [0.1.2] — 2026-06-29

### Added
- Marketing website deploy: static Next.js export served by nginx
  (`site/Dockerfile`), published as `ghcr.io/ravencloak-org/tiny-site` by the
  release workflow, and a `site` service in the prod compose (port 18080).
- API and website split across hosts: site at `tiny.ravencloak.org`, API at
  `tiny-api.ravencloak.org` (first-level subdomain — Cloudflare Universal SSL
  doesn't cover second-level like `api.tiny.*`).

## [0.1.1] — 2026-06-29

### Added
- One-click production stack `deploy/docker-compose.prod.yml` (ClickHouse 26.3 +
  Redis AOF + TinyRaven, health-gated, persistent volumes) for Dokploy Compose deploys.
- Docker image now bakes the `examples/quickstart` project at `/project` and
  defaults `TR_PROJECT_DIR=/project`, so a fresh deploy is queryable immediately.
- Marketing site under `site/` (Next.js + shadcn + Bklit charts): hero, features,
  one-env-var migration, illustrative price comparison, real benchmark
  (~177k events/s, p95 71ms — measured: 50 clients, batched, 1 node, 2.65M events
  persisted to ClickHouse).
- Deploy guide `docs/deploy/dokploy.md` + Cloudflare Tunnel config for `tiny.ravencloak.org`.

### Changed
- Release image is single-arch `linux/amd64` (x86) — dropped multi-arch/QEMU.

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

[0.3.0]: https://github.com/ravencloak-org/tiny/releases/tag/v0.3.0
[0.2.0]: https://github.com/ravencloak-org/tiny/releases/tag/v0.2.0
[0.1.2]: https://github.com/ravencloak-org/tiny/releases/tag/v0.1.2
[0.1.1]: https://github.com/ravencloak-org/tiny/releases/tag/v0.1.1
[0.1.0]: https://github.com/ravencloak-org/tiny/releases/tag/v0.1.0
