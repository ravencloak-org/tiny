# TinyRaven — Project Starting Prompt

> Use this as your first message when opening a new conversation about TinyRaven.
> It contains all architectural decisions, constraints, and context from the planning session.

---

## What is TinyRaven?

TinyRaven is an **open-source, self-hosted, drop-in alternative to Tinybird**, built entirely in **Go** on top of **open-source ClickHouse**. It replicates Tinybird's full developer experience — HTTP ingestion API, SQL pipes, REST endpoint publishing, CLI, branching, and git workflows — but runs on infrastructure you own.

**Inspiration model:** ScyllaDB → Cassandra. Identical API surface, faster/leaner internals, fully open.

**GitHub:** `github.com/tinyraven/tinyraven`  
**Website:** `tinyraven.io`  
**License:** Apache 2.0

---

## Language & Stack Decisions (Final, Do Not Revisit)

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| **Backend server** | Go (`net/http` + `chi` router) | Goroutines ideal for the Gatherer (concurrent batching), single binary, fast startup, low memory for K8s |
| **CLI binary** | Go (same binary as server, `tr` subcommand) — `cobra` (commands) + `viper` (config: `~/.tinyraven/config.yml` ⊕ `TINYBIRD_*` env ⊕ flags) | Standard Go CLI stack (kubectl/gh/docker). OpenTUI rejected: TypeScript (breaks Go-only) and a full-screen TUI framework — `tr` is a command CLI, not a TUI. |
| **Database** | ClickHouse OSS (Apache 2.0), **target 26.3 LTS** (pin `clickhouse/clickhouse-server:26.3`) | Same OLAP engine Tinybird uses, free to self-host. LTS floor lets us build on query_cache, refreshable MV, native JSON, parameterized queries. See `docs/adr/0009-clickhouse-26.3-lts-feature-baseline.md`. |
| **Metadata store** | Redis (AOF-persisted) | Schema registry, token storage, pipe definitions, deploy state. Matches Tinybird. See `docs/adr/0001-redis-only-metadata.md`. |
| **Cache** | Redis | Token validation cache + rate limiting only. **Query-result caching = ClickHouse native `query_cache`** (per-pipe TTL), not Redis. See `docs/adr/0009`. |
| ~~**Object storage**~~ | — (out of scope) | Cold-storage tiering is a ClickHouse `storage_policy` (S3 disk) configured at the ClickHouse level — TinyRaven writes no code for it. Documented as a deploy recipe later, not a feature. |

**Why Go, not Kotlin:** The core work is I/O + ClickHouse proxying + event batching — not complex business logic. Goroutines + channels are a natural fit for the Gatherer. Single binary deployment beats JVM cold start for Kubernetes. Kotlin was rejected despite existing SVOD platform being Kotlin, because TinyRaven is a separate, independently deployed service.

**HTTP Framework:** `net/http` + `chi` router (minimal, idiomatic, MIT licensed). chi is a thin router over stdlib `http.Handler`, not a framework.

**ClickHouse access (see `docs/adr/0013-clickhouse-access-split-native-insert-http-query.md`):** split — `clickhouse-go` v2 native driver (TCP 9000) for Gatherer batched inserts; ClickHouse HTTP interface (8123) via stdlib `net/http` for `/v0/pipes` + `/v0/sql` (`.json` → CH `FORMAT JSON` envelope passthrough, `.ndjson` → `JSONEachRow`, `.csv` → `CSVWithNames`; header passthrough, readonly user; no injected LIMIT, ceiling via CH profile. ADR 0025).

```go
import (
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
)
```

### Locked Dependencies (lean — prefer stdlib, reuse before reinvent)

| Concern | Choice | Note |
|---------|--------|------|
| Router | `github.com/go-chi/chi/v5` | Final (CLAUDE.md). |
| Rate limiting | `github.com/go-chi/httprate` | In-memory sliding-window, per-token. ADR 0015. |
| ClickHouse insert | `github.com/ClickHouse/clickhouse-go/v2` | Native driver. ADR 0013. |
| ClickHouse query | stdlib `net/http` → CH HTTP 8123 | ADR 0013. |
| Redis | `github.com/redis/go-redis/v9` | Metadata registry + hot cache. ADR 0001. |
| CLI / config | `cobra` + `viper` | Final. |
| Metrics | `github.com/prometheus/client_golang` | `/v0/metrics` exposition — never hand-format. |
| Template control flow (Phase 2) | `github.com/expr-lang/expr` | `{% if %}` condition eval — no hand-rolled evaluator. ADR 0003. |
| OpenAPI spec | `github.com/getkin/kin-openapi` | Runtime build from pipe registry. ADR 0017. |
| File watching (dev) | `github.com/fsnotify/fsnotify` | Dev-only hot reload, debounced ~300ms. ADR 0020. mtime-poll is the zero-dep fallback. |
| Logging | stdlib `log/slog` | No zap/zerolog. |
| JSON / NDJSON | stdlib `encoding/json` | `goccy/go-json` only if profiling demands. |
| Request decompression | stdlib `compress/gzip` + `klauspost/compress/zstd` | `/v0/events` `Content-Encoding: gzip`/`zstd`. zstd is transitive via `clickhouse-go` — no new direct dep. chi does **not** decompress requests. ADR 0023. |
| CORS | `github.com/go-chi/cors` | Browser access to pipes/sql. Configurable origins, **default off**. JWT browser tokens deferred to Phase 2. ADR 0026. |

No new dependency is added for what a few lines of stdlib already do.

---

## CLI: `tr` Binary

- **Binary name:** `tr`
- **Package name (install):** `tinyraven` (not `tr`)
- **Tinybird CLI uses** `tb` — we do NOT use `tb` to avoid installation conflicts if both exist
- `tr` conflicts with Unix `tr` (translate characters) only superficially — they operate in completely different domains (Unix `tr` is a stdin filter, TinyRaven `tr` is a subcommand CLI)
- Optional alias: `tb-tr` for users running both Tinybird and TinyRaven side-by-side

```bash
# Installation gives the user:
brew install tinyraven    # installs "tr" binary
tr local start            # starts local dev stack
tr deploy                 # deploy project
tr --version
```

---

## Drop-In Tinybird Compatibility

TinyRaven exposes **identical APIs** to Tinybird. Existing Tinybird client code works unchanged by changing one environment variable.

### API Endpoints (Exact Parity)

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/v0/events` | POST | JSON/NDJSON event ingestion |
| `/v0/pipes/{name}.json` | GET | Parameterized SQL query execution |
| `/v0/sql` | GET | Direct SQL (read-only ClickHouse proxy) |
| `/health` | GET | Liveness — `200` if process up, zero dep checks. ADR 0024. |
| `/health/ready` | GET | Readiness — `200`/`503` gated on Redis + ClickHouse (cached ~2–3s). ADR 0024. |
| `/v0/metrics` | GET | Prometheus format metrics (via `prometheus/client_golang`) |

### File Format Parity

`.datasource` files (see `docs/adr/0008-datasource-reject-undefined-no-schema-on-write.md`): default engine `MergeTree ORDER BY tuple()` when `ENGINE` omitted; all `ENGINE_*` forwarded to ClickHouse verbatim. Ingestion to an undefined datasource is rejected (no schema-on-write in MVP).
```
SCHEMA >
  event_id String,
  user_id String,
  timestamp DateTime,
  properties JSON

ENGINE = MergeTree
ENGINE_SORTING_KEY = (user_id, timestamp)
ENGINE_PARTITION_KEY = toYYYYMM(timestamp)
ENGINE_TTL = timestamp + interval 90 day
CONNECTOR = http_api
```

`.pipe` files:
```
NODE daily_activity
SQL >
  SELECT toDate(timestamp) as date, user_id, count() as events
  FROM events WHERE timestamp >= {{DateTime(start_date)}}
  GROUP BY date, user_id

ENDPOINT user_stats
TYPE query
SQL > SELECT * FROM daily_activity WHERE user_id = {{String(user_id)}}
RATE_LIMIT = 100

MATERIALIZATION daily_summary
TARGET_TABLE daily_metrics
SQL > SELECT * FROM daily_activity
```

### Config/Env Parity

```bash
# Users only need to change TINYBIRD_HOST — everything else identical
export TINYBIRD_HOST=https://tinyraven.example.com
export TINYBIRD_TOKEN=new_token
tr deploy   # same project files, different backend
```

**Config split (secret vs non-secret):**
- `~/.tinyraven/config.yml` — user/home, **gitignored**. Holds the **token** and **host** (secrets + per-machine creds). Same format as `~/.tinybird/config.yml`. Shared across projects.
- `.tr/config.yml` — project, **committed**. Holds project identity + non-secret deploy config (project name, `datasources/`/`pipes/` dirs, safe ClickHouse settings). **Never a token.**
- Precedence (viper): flag > `TINYBIRD_*` env > `.tr/config.yml` > `~/.tinyraven/config.yml` > defaults. Token resolves from env > user file only (never the committed project file).
- `tr init` writes a `.gitignore` guarding any local secret file.

---

## Core Architecture

### Project File Structure
```
my-project/
├── .tr/config.yml
├── datasources/
│   └── events.datasource
├── pipes/
│   ├── user_metrics.pipe
│   └── materialized/daily_summary.pipe
├── tests/
└── .github/workflows/ci.yml
```

### Key Technical Patterns

**Gatherer (event batching):**
```go
// Go channel + goroutine flushes on max(N events, 5s timeout)
type Gatherer struct {
    buffer  chan Event
    size    int           // flush at N events
    timeout time.Duration // flush after 5s
    ch      *ClickHouseClient
}
```
- **Delivery contract (see `docs/adr/0004-ingestion-ack-on-buffer.md`):** ack-on-buffer → `/v0/events` returns **202 Accepted**. Graceful drain on SIGTERM (no loss on restart). Hard crash loses up to one batch window (at-most-once) until the optional disk WAL ships (Phase 2/3).
- **Validation + quarantine (see `docs/adr/0018-events-quarantine-validate-in-go.md`):** per-row validation in Go pre-buffer; schema-invalid **and** unparseable rows → `{ds}_quarantine` table, never batch-rejected; `202 {successful_rows, quarantined_rows}` even when zero succeed. `name=` required (400).
- **Compression (see `docs/adr/0023-events-request-compression.md`):** `Content-Encoding: gzip`/`zstd` auto-decompressed. Wire cap (default 10 MB) on **compressed** bytes → 413; separate **decompressed** ceiling (default 256 MB, streamed) guards decompression bombs; unsupported encoding → 415.

**Pipe templating (see `docs/adr/0003-pipe-templating-parameterized-queries.md`):**
- **MVP (Phase 1): value params only.** `{{Type(name, default)}}` → ClickHouse parameterized query `{name:Type}` + params map (`param_<name>=...`). No string interpolation — injection-proof by construction. Never `text/template`. Pure substitution, zero parser.
- **Control flow `{% if %}` / `{% for %}` / `defined()` deferred to Phase 2.** Thin block tokenizer delegating condition evaluation to `github.com/expr-lang/expr` (sandboxed) — no hand-rolled expression evaluator. Structural identifiers allowlisted.
- Full function catalog (long tail) deferred to Phase 2+.
- Execute via ClickHouse HTTP interface, return `FORMAT JSONEachRow`.

**Auth tokens (RBAC) — see `docs/adr/0005-opaque-tokens-redis.md`:**
- Opaque random bearer tokens; scopes stored in Redis (persistent record + short-TTL validation cache). No JWT in MVP.
- Static/admin tokens never expire (TTL only on cache + short-lived client tokens).
- Bootstrap: auto-generate `ADMIN` token on first init, print once, write to `~/.tinyraven/config.yml` (idempotent).
- MVP scopes: `ADMIN`, `WORKSPACE:READ_ALL`, per-pipe `READ`. Resource tokens declared in `.pipe`/`.datasource`, materialized on `tr deploy`.

**SQL read-only enforcement (see `docs/adr/0011-sql-readonly-via-clickhouse-profile.md`):** `/v0/sql` + pipe reads run under a dedicated ClickHouse `readonly=2` user + resource caps — ClickHouse enforces, TinyRaven never parses SQL to block writes. Separate read-write CH user for Gatherer + `tr deploy` DDL.

**Pipe result governance + browser access (see `docs/adr/0025-pipe-result-governance-cors-query-token.md`):** no framework-injected `LIMIT` (pipe SQL authoritative — parity); pagination is the author's job via value-params (`LIMIT {{Int(limit,100)}} OFFSET {{Int(offset,0)}}`). Server guardrails = CH settings on the readonly profile (`max_result_rows` / `max_result_bytes` / `max_execution_time`), over-cap → CH error → error parity (ADR 0012). CORS enabled on `/v0/pipes/*` GET; `?token=` query param accepted (simple GET, no preflight, no `go-chi/cors` dep) — flagged: query-string token leaks to logs/`Referer`, so Bearer for server-to-server, `?token=` (resource-scoped) only for browser embedding.

**Error contract (see `docs/adr/0012-structural-error-parity.md`):** structural parity with Tinybird — JSON `{"error":"<msg>"}`, status mapping (400/401/403/404/429/500), and `X-DB-Exception-Code` header passing through the ClickHouse exception code. Structure + codes, not message text.

**Branching (see `docs/adr/0007-branch-schema-only-explicit-lifecycle.md`):**
- `CREATE DATABASE tr_{branch}` on branch create (branch name sanitized to a valid identifier). **Schema-only — no data copied.** Prod data never auto-copied; `--with-sample N` fixtures are a later add.
- Lifecycle is **explicit**: `tr branch rm <name>` drops the DB. No "drop on merge" (merge detection is unreliable). Optional `tr branch prune` drops orphaned branch DBs after confirmation.
- `tr local start --branch feature-x` targets branch DB

**Schema migrations (see `docs/adr/0006-migration-safe-auto-breaking-refuse.md`):**
- `tr deploy` diffs project files vs live ClickHouse and classifies each change.
- Safe (auto-apply): add nullable/defaulted column.
- Breaking (drop/rename column, type change, sorting/partition key change, engine change): **refused by default** — print diff + reason, exit non-zero. `--dry-run` prints diff, applies nothing.
- Phase 3: guarded breaking path (shadow table → MV backfill → atomic `EXCHANGE TABLES`) behind `tr deploy --allow-breaking`.

---

## Tinybird Internals (Publicly Known — We Replicate These)

| Tinybird Component | TinyRaven Implementation |
|-------------------|-------------------------|
| ClickHouse fork (private) | Open-source ClickHouse (Apache 2.0) |
| Gatherer (event buffer) | Go channel + goroutine batch flusher |
| Packed part format | Standard ClickHouse parts (we skip the fork optimization) |
| Zero-copy replication | Standard ClickHouse replication |
| Pipes → REST API | Go handler: SQL template → param injection → CH HTTP → JSON |
| Materialized Views | Standard ClickHouse `CREATE MATERIALIZED VIEW` |
| Branches | `CREATE DATABASE tr_{branch}` per git branch |
| Token auth + RBAC | Go middleware + Redis |
| Pipe stats / observability | `tinybird.pipe_stats` ClickHouse table, fed through the Gatherer as an internal datasource (async, best-effort — see `docs/adr/0014-pipe-stats-via-gatherer.md`) |

**Tinybird's actual stack (for reference):** Python backend, C++ ClickHouse fork, Next.js UI, OpenResty LB, Redis, Kubernetes. We replace all of this with Go + OSS ClickHouse.

---

## Frontend / Dashboard Strategy

**Decision: TinyRaven is API-first. No built-in dashboard.**

This mirrors Tinybird's own approach — they do not ship a dashboard UI either. Tinybird recommends Next.js + Tremor + Recharts for users who want dashboards.

**TinyRaven approach:**
- Phase 1–3: API-only (call `/v0/pipes/{name}.json` from any frontend)
- Phase 4: Optional open-source Next.js dashboard template in a separate repo
- BI tools (Metabase, Superset, Grafana) connect directly to ClickHouse — zero extra work needed

**Recommended dashboard stack for users:**
- Next.js + Recharts (same as Tinybird Web Analytics Starter Kit)
- Metabase (drag-drop, connects to ClickHouse natively)
- Apache Superset (SQL-first, self-hosted)
- Grafana (metrics/timeseries)

---

## Distribution & Installation

### Package Name vs Binary Name

| Manager | Package Name | Binary |
|---------|-------------|--------|
| Homebrew | `brew install tinyraven` | `tr` |
| APT | `sudo apt install tinyraven` | `tr` |
| SDKMAN | `sdk install tinyraven` | `tr` |
| Docker | `ghcr.io/tinyraven/tinyraven:latest` | `tr` (inside container) |
| Helm | `helm install tinyraven/tinyraven` | Pod runs `tr` binary |

Package is always `tinyraven`, binary is always `tr`.

### Homebrew Tap
```bash
brew tap tinyraven/tinyraven
brew install tinyraven
# User now has "tr" binary
```

Formula name: `tinyraven` → auto-updated via GitHub Actions on new releases.

### APT Repository
```bash
echo "deb https://repo.tinyraven.io/deb focal main" | sudo tee /etc/apt/sources.list.d/tinyraven.list
sudo apt-get update && sudo apt-get install tinyraven
```

### SDKMAN
```bash
sdk install tinyraven
```

### Docker
```bash
docker run -p 8000:8000 \
  -e CLICKHOUSE_HOST=clickhouse:9000 \
  ghcr.io/tinyraven/tinyraven:latest
```

### One-Click Cloud Deploy Buttons (README)

```markdown
[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https://github.com/tinyraven/tinyraven)

[![Launch on AWS](https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png)](https://console.aws.amazon.com/cloudformation/home?region=us-east-1#/stacks/new?stackName=tinyraven&templateURL=https://tinyraven-cfn.s3.amazonaws.com/tinyraven-template.yaml)

[Deploy to Railway](https://railway.app/new?repo=https://github.com/tinyraven/tinyraven)

[Deploy to DigitalOcean](https://cloud.digitalocean.com/apps/new?repo=https://github.com/tinyraven/tinyraven)
```

**Heroku mechanism:** `app.json` in repo root → defines add-ons (Redis, AOF-persisted), env vars (CLICKHOUSE_HOST), buildpack (heroku/go). User clicks → configures env vars → deploys in ~5 minutes.

**AWS mechanism:** CloudFormation template (`cloudformation/tinyraven-template.yaml`) → provisions VPC, EC2 (downloads Go binary), ElastiCache/Redis (persisted), Elastic IP → outputs TinyRaven URL. ~10-15 minutes.

---

## Development Phases & Milestones

### Phase 1: MVP — Core Ingestion + Query (Weeks 1–2)

**Goal:** Working `tr local` dev environment

**Deliverables:**
- [ ] Go HTTP server (`net/http` + `chi`) with `/v0/events`, `/v0/pipes/:name.json`, `/health` (liveness) + `/health/ready` (readiness: Redis + ClickHouse, ADR 0024)
- [ ] Gatherer: in-memory ring buffer, flush on `max(10,000 events, 5s timeout)`
- [ ] Datasource schema registry (parse `.datasource` files, store in Redis)
- [ ] Basic pipe executor: parse `{{Type(param)}}` SQL templates, inject validated params, execute via ClickHouse HTTP
- [ ] Token auth middleware (Bearer tokens → Redis lookup)
- [ ] `tr local start` — Docker Compose stack (ClickHouse + TinyRaven + Redis)
- [ ] Hot reload (dev-only, `fsnotify`): `.pipe` change → atomic registry swap (instant); `.datasource` change → route through `tr deploy` safe-migration path, not instant DDL. See `docs/adr/0020-hot-reload-dev-only-pipes-instant-datasources-via-deploy.md`

**Success criteria:** POST events → Gatherer → ClickHouse → GET pipe → JSON response

---

### Phase 2: API Publishing + Deployment (Weeks 3–4)

**Goal:** Production-ready query API, deployable project

**Deliverables:**
- [ ] Parameterized SQL pipes with `{{Type(name, default)}}` syntax (full type support: String, DateTime, Int64, Float64, UUID, Boolean)
- [ ] API endpoint publishing (`GET /v0/pipes/{name}.json`)
- [ ] Auto-generated OpenAPI documentation (scan pipe registry → generate spec)
- [ ] Query observability: log every query to `tinybird.pipe_stats` ClickHouse table
- [ ] `GET /v0/metrics` in Prometheus format
- [ ] `tr deploy`: validate `.datasource` / `.pipe` files, diff schema, apply safe migrations (`ALTER TABLE ADD COLUMN`)
- [ ] Per-token rate limiting (Redis counter, configurable per pipe via `RATE_LIMIT`)
- [ ] `/v0/sql` read-only ClickHouse SQL proxy
- [ ] Error handling with Tinybird-compatible error codes and JSON shapes

**Success criteria:** Full round-trip from `tr deploy` to queryable API endpoint

---

### Phase 3: Dev Workflows + Branching (Weeks 5–6)

**Goal:** Full development workflow parity with Tinybird

**Deliverables:**
- [ ] Branches: isolated ClickHouse database per git branch (`tr_main`, `tr_feature_x`)
- [ ] `tr local start --branch feature-x` (preview environment)
- [ ] `tr deploy` detects current git branch, targets correct workspace
- [ ] Materialized views: auto-create ClickHouse MVs from pipes marked `TYPE materialization`
- [ ] Breaking schema migrations: shadow table → backfill MV → `EXCHANGE TABLES` atomically
- [ ] Config file support (`~/.tinyraven/config.yml`, same format as Tinybird)
- [ ] Environment variable override (`TINYBIRD_HOST`, `TINYBIRD_TOKEN`, `TINYBIRD_WORKSPACE` all supported)
- [ ] GitHub Actions CI/CD templates (validate on PR, deploy on merge to main)

**Success criteria:** Developer can work across feature branches with isolated data, merge and deploy without data loss

---

### Phase 4: Distribution + Cloud Deploy (Weeks 7–8)

**Goal:** Users can install TinyRaven in one command on any platform, or deploy to cloud with one click

**Deliverables:**

**GoReleaser setup:**
- [ ] Cross-platform binaries: Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64)
- [ ] DEB and RPM packages via `nfpms`
- [ ] SHA256 checksums + GPG signing for all artifacts
- [ ] GitHub Releases automation on `git tag vX.Y.Z`

**Package Manager Distribution:**
- [ ] Homebrew tap (`tinyraven/homebrew-tinyraven`) with `tinyraven` formula
- [ ] GitHub Actions auto-updates tap on new release
- [ ] APT repository hosted on GitHub Pages (`repo.tinyraven.io/deb`)
- [ ] SDKMAN vendor registration
- [ ] Docker multi-arch image (`ghcr.io/tinyraven/tinyraven`) — `linux/amd64`, `linux/arm64`
- [ ] GitHub Container Registry auto-publish on tag

**One-Click Cloud Deploy:**
- [ ] `app.json` in repo root (Heroku Button)
  - Add-ons: `heroku-redis:mini` (AOF-persisted; system of record for metadata)
  - Env vars: `CLICKHOUSE_HOST` (required), `TINYRAVEN_PORT`, `TINYRAVEN_ENV`
  - Buildpack: `heroku/go`
- [ ] `cloudformation/tinyraven-template.yaml` (AWS)
  - Resources: VPC, PublicSubnet, EC2 (UserData downloads binary + starts systemd service), ElastiCache/Redis (persisted), ElasticIP
  - Parameters: InstanceType (default `t3.medium`), KeyPair, ClickHouse endpoint
  - Outputs: TinyRavenURL, DatabaseEndpoint, SSHCommand
  - Upload template to S3 → generate CloudFormation quick-launch URL
- [ ] `railway.json` (Railway)
- [ ] `app.yaml` (DigitalOcean App Platform)
- [ ] `docker-compose.yml` (VPS/self-hosted, includes ClickHouse + TinyRaven + Redis)

**Kubernetes:**
- [ ] Helm chart (`charts/tinyraven/`) with `values.yaml` defaults
- [ ] Publish chart to GitHub Pages Helm repo

**Documentation:**
- [ ] `docs/deploy/heroku.md`
- [ ] `docs/deploy/aws.md`
- [ ] `docs/deploy/railway.md`
- [ ] `docs/deploy/docker.md`
- [ ] `docs/deploy/kubernetes.md`
- [ ] Migration guide from Tinybird (`docs/migrate-from-tinybird.md`)

**Success criteria:** `brew install tinyraven` → `tr local start` works in < 2 minutes; Heroku button deploys in < 5 minutes; AWS CloudFormation stack completes in < 15 minutes

---

### Phase 5: Connectors + Community (Weeks 9+)

**Goal:** Production readiness, ecosystem

**Deliverables:** (connectors = ClickHouse-native engines, not built services — see `docs/adr/0019-connectors-via-clickhouse-engines.md`)
- [ ] Kafka: `.datasource` template for `ENGINE = Kafka(...)` + MV; `tr deploy` creates CH objects (CH runs the consumer)
- [ ] S3 / files: `.datasource` templates for `s3()`/`url()`/`file()` + `ENGINE = S3`
- [ ] PostgreSQL: `ENGINE = PostgreSQL(...)` / `postgresql()` table function (optional CDC via `MaterializedPostgreSQL`)
- [ ] ClickHouse HTTP interface compatibility for BI tools (DBeaver, Grafana, Superset, Metabase)
- [ ] Integration tests (end-to-end: event → query → result)
- [ ] Load testing benchmarks (throughput, latency at 10k events/s)
- [ ] Optional Next.js dashboard template (separate repo: `tinyraven/dashboard-template`)
- [ ] Plugin template library
- [ ] Community Slack / GitHub Discussions

**Success criteria:** TinyRaven handles ≥ 10k events/second on a single t3.large EC2 instance

---

## Pending Decisions (Not Yet Made)

- [x] **SQL template parser**: RESOLVED → MVP ships value params only (pure CH-parameterized substitution, no parser). Control flow deferred to Phase 2, conditions via `github.com/expr-lang/expr` (no hand-rolled evaluator). See `docs/adr/0003-pipe-templating-parameterized-queries.md`.
- [x] **Metadata storage**: RESOLVED → Redis only (AOF-persisted), matching Tinybird. Postgres dropped. See `docs/adr/0001-redis-only-metadata.md`.
- [ ] **Installation script**: `curl https://tinyraven.io/install.sh | bash` — implement in Phase 4?
- [ ] **Tinybird CLI passthrough mode**: support `TINYRAVEN_PASSTHROUGH=true` to forward requests to real Tinybird (for gradual migration)?

---

## What NOT to Build (Intentional Scope Limits)

- **No built-in dashboard UI** (API-first; Metabase/Superset connect directly to ClickHouse)
- **No managed cloud offering** (pure self-hosted FOSS)
- **No ClickHouse fork** (use OSS ClickHouse as-is; skip packed part format and zero-copy optimizations)
- **No AI/LLM features** in MVP (Tinybird has "Tinybird Code" AI agent — defer this to community)
- **No Kotlin code** (TinyRaven is 100% Go; the SVOD platform stays Kotlin separately)
- **No object-storage management** (S3/MinIO cold tiering is a ClickHouse `storage_policy` config, not TinyRaven code — deploy-recipe doc only)

---

## Differentiators vs Tinybird

| | Tinybird | TinyRaven |
|--|---------|----------|
| **License** | Proprietary SaaS (TSML for local) | Apache 2.0 |
| **Hosting** | Managed cloud only (self-managed in beta) | Self-hosted from day one |
| **Database** | Private ClickHouse fork | OSS ClickHouse (Apache 2.0) |
| **Backend** | Python + C++ | Go (single binary) |
| **Cost** | Usage-based pricing | Pay only for infrastructure |
| **API compatibility** | — | 100% Tinybird-compatible |
| **CLI** | `tb` (Python) | `tr` (Go, single binary) |

---

## Integration with SVOD Platform

TinyRaven is a **separate service** from the Kotlin SVOD platform. They communicate over HTTP:

```kotlin
// In SVOD (Kotlin/Ktor) — calls TinyRaven API for analytics
val response = httpClient.get("https://tinyraven.example.com/v0/pipes/user_watch_history.json") {
    parameter("user_id", userId)
    header("Authorization", "Bearer $token")
}
```

```go
// TinyRaven sends analytics events from SVOD via ingestion API
// SVOD → POST /v0/events → Gatherer → ClickHouse → Python recommendation microservice
```

SVOD platform handles: video delivery, user auth, transcoding, metadata (PostgreSQL via Exposed).  
TinyRaven handles: analytics ingestion, aggregation, recommendation data via ClickHouse pipes.

---

## Go Project Structure (Starting Point)

```
tinyraven/
├── cmd/
│   └── tr/
│       └── main.go               # CLI entrypoint
├── internal/
│   ├── api/
│   │   ├── server.go             # chi router setup
│   │   ├── handlers/
│   │   │   ├── events.go         # POST /v0/events
│   │   │   ├── pipes.go          # GET /v0/pipes/:name.json
│   │   │   ├── sql.go            # GET /v0/sql
│   │   │   └── health.go         # GET /health
│   │   └── middleware/
│   │       ├── auth.go           # Bearer token validation
│   │       └── ratelimit.go      # Per-token rate limiting
│   ├── gatherer/
│   │   └── gatherer.go           # Event batching + ClickHouse flush
│   ├── pipe/
│   │   ├── parser.go             # Parse .pipe files
│   │   ├── executor.go           # SQL template injection + execution
│   │   └── registry.go           # In-memory pipe store
│   ├── datasource/
│   │   ├── parser.go             # Parse .datasource files
│   │   └── registry.go           # Redis-backed schema store
│   ├── clickhouse/
│   │   └── client.go             # ClickHouse HTTP client
│   └── auth/
│       └── tokens.go             # Token store (Redis)
├── cmd/tr/
│   ├── local.go                  # tr local start
│   ├── deploy.go                 # tr deploy
│   ├── login.go                  # tr login
│   └── status.go                 # tr status
├── docker-compose.yml            # Local dev stack
├── app.json                      # Heroku Button config
├── cloudformation/
│   └── tinyraven-template.yaml   # AWS CloudFormation
├── railway.json                  # Railway deploy
├── app.yaml                      # DigitalOcean
├── charts/tinyraven/             # Helm chart
├── .goreleaser.yaml              # Cross-platform build + packaging
└── README.md                     # Deploy buttons + quickstart
```

---

## Quick Reference Commands

```bash
# Local development
tr local start                    # Spin up ClickHouse + TinyRaven + Redis
tr local start --branch feature   # Spin up with isolated branch DB
tr deploy                         # Deploy .datasource + .pipe files
tr status                         # Show workspace status
tr login                          # Authenticate with TinyRaven server

# API usage (same as Tinybird)
curl -X POST "http://localhost:8000/v0/events?name=events" \
  -H "Authorization: Bearer $TR_TOKEN" \
  -d '{"user_id":"alice","event":"page_view","timestamp":"2026-01-01T00:00:00Z"}'

curl "http://localhost:8000/v0/pipes/user_metrics.json?user_id=alice&start_date=2026-01-01" \
  -H "Authorization: Bearer $TR_TOKEN"

# Installation (once published)
brew install tinyraven             # macOS / Linux (Homebrew)
sudo apt install tinyraven         # Debian / Ubuntu
sdk install tinyraven              # SDKMAN
docker pull ghcr.io/tinyraven/tinyraven:latest
```

---

*This document represents all decisions made as of June 2026. When starting a new conversation, paste this file as context. Do not re-litigate decisions already made (Go language, `tr` CLI name, `tinyraven` package name, API-first frontend approach). Build on top of them.*
