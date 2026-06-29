# TinyRaven — Development Milestones

> Condensed milestone summary per phase. Each phase has a clear deliverable and success criteria.
> Track progress by checking items as they ship.

---

## Phase 1 — MVP: Core Ingestion + Query
**Timeline:** Weeks 1–2  
**Deliverable:** Working `tr local` dev environment — events in, query results out

### Must Ship
- [x] Go HTTP server (`net/http` + `chi` router)
- [x] `POST /v0/events` — JSON / NDJSON ingestion
- [x] `GET /v0/pipes/:name.json` — parameterized SQL query
- [x] `GET /health` (liveness) + `GET /health/ready` (readiness: Redis + ClickHouse) — see ADR 0024
- [x] Gatherer: in-memory buffer, flush on `max(10,000 events, 5s timeout)`
- [x] `.datasource` file parser → Redis schema registry (ADR 0001 — Redis-only metadata)
- [x] `.pipe` file parser → `{{Type(param, default)}}` SQL template injection
- [x] Bearer token auth middleware → Redis lookup
- [x] `tr local start` → Docker Compose (ClickHouse + TinyRaven + Redis)
- [x] Hot reload on `.datasource` / `.pipe` file change (mtime poll; fsnotify upgrade later)

### Success Criteria
```bash
curl -X POST localhost:8000/v0/events?name=events \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"user_id":"alice","event":"page_view"}'
# → 202 {"successful_rows":1,"quarantined_rows":0}  (ack-on-buffer ADR 0004; quarantine ADR 0018)

curl "localhost:8000/v0/pipes/user_metrics.json?user_id=alice" \
  -H "Authorization: Bearer $TOKEN"
# → [{"user_id":"alice","count":1}]
```

---

## Phase 2 — API Publishing + Deployment
**Timeline:** Weeks 3–4  
**Deliverable:** Production-ready query API + deployable project via `tr deploy`

### Must Ship
- [x] Full SQL param type support: `String`, `DateTime`, `Int64`, `Float64`, `UUID`, `Boolean` (+ Int32/Date/DateTime64)
- [x] Auto-generated OpenAPI docs from pipe registry (ADR 0017, `/v0/openapi.json`)
- [x] Query observability → `pipe_stats` ClickHouse table (via Gatherer-style flusher, ADR 0014)
- [x] `GET /v0/metrics` → Prometheus format
- [x] `GET /v0/sql` → read-only ClickHouse SQL proxy (`readonly=2`, ADR 0011)
- [x] `tr deploy` command:
  - [x] Validate `.datasource` + `.pipe` files
  - [x] Diff schema against current ClickHouse state (`system.columns`)
  - [x] Apply safe migrations (`ALTER TABLE ADD COLUMN`); breaking changes detected + refused (Phase 3, ADR 0007)
- [x] Per-token rate limiting (in-memory `httprate` sliding-window, ADR 0015; per-pipe `RATE_LIMIT` wiring later)
- [x] Tinybird-compatible error codes + JSON error shapes (ADR 0012, `X-DB-Exception-Code` passthrough)

### Success Criteria
```bash
tr deploy
# ✓ Validated 2 datasources, 3 pipes
# ✓ Applied 1 migration: ALTER TABLE events ADD COLUMN country String DEFAULT ''
# ✓ Published 3 endpoints

curl localhost:8000/v0/metrics
# # HELP tinyraven_requests_total...
# tinyraven_requests_total{pipe="user_metrics"} 42
```

---

## Phase 3 — Dev Workflows + Branching
**Timeline:** Weeks 5–6  
**Deliverable:** Full development workflow parity with Tinybird

### Must Ship
- [ ] Branch isolation: `CREATE DATABASE tr_{branch}` per git branch (single-tenant; workspace = deployment)
- [ ] `tr local start --branch feature-x` → isolated ClickHouse DB
- [ ] `tr deploy` detects git branch → targets correct workspace DB
- [ ] Materialized views from `.pipe` files with `TYPE materialization` + `TARGET_TABLE`
- [ ] Breaking schema migrations: shadow table → MV backfill → `EXCHANGE TABLES` (atomic swap)
- [ ] `~/.tinyraven/config.yml` read/write (same format as Tinybird's `~/.tinybird/config.yml`)
- [ ] Env var support: `TINYBIRD_HOST`, `TINYBIRD_TOKEN`, `TINYBIRD_WORKSPACE` (all honoured)
- [ ] GitHub Actions CI/CD template: validate on PR, deploy on merge to main

### Success Criteria
```bash
git checkout -b feature-new-metric
tr local start --branch feature-new-metric
# ✓ Started TinyRaven on tr_feature_new_metric

# Edit user_metrics.pipe, add new field
tr deploy
# ✓ Branch deploy: tr_feature_new_metric updated

git checkout main && git merge feature-new-metric
tr deploy
# ✓ Production deploy: tr_main updated (zero downtime, EXCHANGE TABLES)
```

---

## Phase 4 — Distribution + Cloud Deploy
**Timeline:** Weeks 7–8  
**Deliverable:** One-command install on any platform; one-click deploy to Heroku or AWS

### Must Ship

#### Build Pipeline
- [ ] GoReleaser config: Linux/macOS/Windows × amd64/arm64 binaries
- [ ] DEB packages via `nfpms` (Debian/Ubuntu)
- [ ] RPM packages (RHEL/Fedora)
- [ ] SHA256 checksums + GPG signing
- [ ] GitHub Actions: auto-release on `git tag vX.Y.Z`

#### Package Managers
- [ ] **Homebrew tap** (`tinyraven/homebrew-tinyraven`): `brew install tinyraven` → installs `tr` binary
- [ ] **APT repo** (GitHub Pages): `sudo apt install tinyraven` → installs `tr` binary
- [ ] **Docker** multi-arch image: `ghcr.io/tinyraven/tinyraven:latest` (amd64 + arm64)
- [ ] GitHub Actions auto-publishes Docker image + Homebrew tap + APT repo on each release

#### One-Click Cloud Deploy
- [ ] **Heroku Button** (`app.json` in repo root):
  - Add-ons: `heroku-redis:mini` (ClickHouse external; no Postgres — ADR 0001)
  - Required env vars: `CLICKHOUSE_HOST`
  - Buildpack: `heroku/go`
  - `[![Deploy to Heroku](...)](https://heroku.com/deploy?template=https://github.com/tinyraven/tinyraven)`
- [ ] **AWS CloudFormation** (`cloudformation/tinyraven-template.yaml`):
  - Provisions: VPC, Subnet, Security Groups, EC2, ElastiCache Redis, Elastic IP
  - UserData: downloads binary, creates systemd service, starts TinyRaven
  - Parameters: `InstanceType` (default `t3.medium`), `RedisEndpoint`, `ClickHouseEndpoint`
  - Outputs: `TinyRavenURL`, `DatabaseEndpoint`, `SSHCommand`
  - Template uploaded to S3 → CloudFormation quick-launch URL in README
- [ ] **Railway** (`railway.json`): `[Deploy to Railway]` button
- [ ] **DigitalOcean** (`app.yaml`): `[Deploy to DigitalOcean]` button
- [ ] **Docker Compose** (`docker-compose.yml`): includes ClickHouse + TinyRaven + Redis

#### Kubernetes
- [ ] Helm chart (`charts/tinyraven/`) with sane `values.yaml` defaults
- [ ] Published to GitHub Pages Helm repo

#### Docs
- [ ] `docs/deploy/heroku.md`
- [ ] `docs/deploy/aws.md`
- [ ] `docs/deploy/railway.md`
- [ ] `docs/deploy/docker.md`
- [ ] `docs/deploy/kubernetes.md`
- [ ] `docs/migrate-from-tinybird.md` — show the 3-step migration path

### Success Criteria
```bash
# Homebrew
brew install tinyraven
tr local start
# ✓ TinyRaven running at localhost:8000

# Heroku
# Click button in README → fill form → 5 minutes → app live

# AWS
# Click Launch Stack → fill params → 15 minutes → stack complete → URL in outputs

# Docker
docker compose up -d
tr deploy
# ✓ Events API ready
```

---

## Phase 5 — Connectors + Community
**Timeline:** Weeks 9+  
**Deliverable:** Production readiness, ecosystem integrations

### Must Ship
> Connectors = ClickHouse-native engines declared in `.datasource`, not built services — see `docs/adr/0019-connectors-via-clickhouse-engines.md`. `tr deploy` creates the CH objects; ClickHouse does the pulling.
- [ ] Kafka source: `.datasource` template for `ENGINE = Kafka(...)` + MV (CH runs the consumer)
- [ ] S3 / files: `.datasource` templates for `s3()` / `url()` / `file()` + `ENGINE = S3`
- [ ] PostgreSQL: `ENGINE = PostgreSQL(...)` / `postgresql()` table function, optional CDC via `MaterializedPostgreSQL`
- [ ] BI tool compatibility: Metabase, Apache Superset, Grafana, DBeaver connect via ClickHouse HTTP interface
- [ ] Integration test suite (end-to-end: ingest → materialize → query)
- [ ] Load test benchmarks: target ≥ 10k events/s on single `t3.large`
- [ ] Optional dashboard template — **separate repo** (`tinyraven/dashboard-template`, Next.js + Recharts). Core stays API-first with **no built-in dashboard** (CLAUDE.md); this is an external, opt-in starter that talks to the API, not a bundled feature.

### Success Criteria
```bash
# Kafka source in .datasource file (CH-native engine — ADR 0019)
ENGINE = Kafka
ENGINE_KAFKA_BROKER_LIST = kafka:9092
ENGINE_KAFKA_TOPIC_LIST = events
ENGINE_KAFKA_GROUP_NAME = tinyraven
ENGINE_KAFKA_FORMAT = JSONEachRow
# + a materialized view from this datasource into the target table

# BI tool (Metabase)
# Connect → ClickHouse → host: clickhouse.tinyraven.local → browse tables → build dashboard

# Load test
wrk -t4 -c100 -d30s -s post_event.lua http://localhost:8000/v0/events
# Requests/sec: 12,450  ← target ≥ 10,000
```

---

## Summary Table

| Phase | Timeline | Key Deliverable | Success Gate |
|-------|----------|----------------|--------------|
| 1 — MVP | Wk 1–2 | `tr local start` + events + pipes | POST → GET round-trip works |
| 2 — API | Wk 3–4 | `tr deploy` + OpenAPI + metrics | Full deploy + query cycle |
| 3 — Workflows | Wk 5–6 | Branches + materialized views | Zero-downtime migration |
| 4 — Distribution | Wk 7–8 | Brew/APT/Heroku/AWS install | 1-click deploy all platforms |
| 5 — Community | Wk 9+ | Connectors + BI integrations | 10k events/s benchmark |

---

## Architectural Constraints (Non-Negotiable)

- **Language:** Go only — no Kotlin, no Python, no JVM in TinyRaven core
- **HTTP router:** `chi` — not gin, not echo, not fiber
- **CLI binary name:** `tr` — package name is `tinyraven`
- **Frontend:** none built-in — API-first, users bring their own dashboards
- **ClickHouse:** OSS only — no forking, no private builds
- **Compatibility:** every Tinybird API endpoint must work identically

---

*Last updated: June 2026*
