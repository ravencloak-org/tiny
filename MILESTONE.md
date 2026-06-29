# TinyRaven — Development Milestones

> Condensed milestone summary per phase. Each phase has a clear deliverable and success criteria.
> Track progress by checking items as they ship.

---

## Phase 1 — MVP: Core Ingestion + Query
**Timeline:** Weeks 1–2  
**Deliverable:** Working `tr local` dev environment — events in, query results out

### Must Ship
- [ ] Go HTTP server (`net/http` + `chi` router)
- [ ] `POST /v0/events` — JSON / NDJSON ingestion
- [ ] `GET /v0/pipes/:name.json` — parameterized SQL query
- [ ] `GET /health` — health check endpoint
- [ ] Gatherer: in-memory ring buffer, flush on `max(10,000 events, 5s timeout)`
- [ ] `.datasource` file parser → PostgreSQL schema registry
- [ ] `.pipe` file parser → `{{Type(param, default)}}` SQL template injection
- [ ] Bearer token auth middleware → Redis lookup
- [ ] `tr local start` → Docker Compose (ClickHouse + TinyRaven + Redis)
- [ ] Hot reload on `.datasource` / `.pipe` file change

### Success Criteria
```bash
curl -X POST localhost:8000/v0/events?name=events \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"user_id":"alice","event":"page_view"}'
# → 202 Accepted {"status":"ok"}   (ack-on-buffer; see docs/adr/0004)

curl "localhost:8000/v0/pipes/user_metrics.json?user_id=alice" \
  -H "Authorization: Bearer $TOKEN"
# → [{"user_id":"alice","count":1}]
```

---

## Phase 2 — API Publishing + Deployment
**Timeline:** Weeks 3–4  
**Deliverable:** Production-ready query API + deployable project via `tr deploy`

### Must Ship
- [ ] Full SQL param type support: `String`, `DateTime`, `Int64`, `Float64`, `UUID`, `Boolean`
- [ ] Auto-generated OpenAPI docs from pipe registry
- [ ] Query observability → `tinybird.pipe_stats` ClickHouse table
- [ ] `GET /v0/metrics` → Prometheus format
- [ ] `GET /v0/sql` → read-only ClickHouse SQL proxy
- [ ] `tr deploy` command:
  - Validate `.datasource` + `.pipe` files
  - Diff schema against current ClickHouse state
  - Apply safe migrations (`ALTER TABLE ADD COLUMN` with nullable/default)
- [ ] Per-token rate limiting (Redis counter, `RATE_LIMIT` in `.pipe` file)
- [ ] Tinybird-compatible error codes + JSON error shapes

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
- [ ] **SDKMAN** vendor registration: `sdk install tinyraven` → installs `tr` binary
- [ ] **Docker** multi-arch image: `ghcr.io/tinyraven/tinyraven:latest` (amd64 + arm64)
- [ ] GitHub Actions auto-publishes Docker image + Homebrew tap + APT repo on each release

#### One-Click Cloud Deploy
- [ ] **Heroku Button** (`app.json` in repo root):
  - Add-ons: `heroku-postgresql:mini`, `heroku-redis:mini`
  - Required env vars: `CLICKHOUSE_HOST`
  - Buildpack: `heroku/go`
  - `[![Deploy to Heroku](...)](https://heroku.com/deploy?template=https://github.com/tinyraven/tinyraven)`
- [ ] **AWS CloudFormation** (`cloudformation/tinyraven-template.yaml`):
  - Provisions: VPC, Subnet, Security Groups, EC2, RDS PostgreSQL, Elastic IP
  - UserData: downloads binary, creates systemd service, starts TinyRaven
  - Parameters: `InstanceType` (default `t3.medium`), RDS credentials, `ClickHouseEndpoint`
  - Outputs: `TinyRavenURL`, `DatabaseEndpoint`, `SSHCommand`
  - Template uploaded to S3 → CloudFormation quick-launch URL in README
- [ ] **Railway** (`railway.json`): `[Deploy to Railway]` button
- [ ] **DigitalOcean** (`app.yaml`): `[Deploy to DigitalOcean]` button
- [ ] **Docker Compose** (`docker-compose.yml`): includes ClickHouse + TinyRaven + Redis + PostgreSQL

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
- [ ] Kafka connector (consumer group, offset management, schema mapping)
- [ ] S3 connector (batch import, scheduled `COPY` pipes)
- [ ] PostgreSQL connector (table function, optional CDC via logical replication)
- [ ] BI tool compatibility: Metabase, Apache Superset, Grafana, DBeaver connect via ClickHouse HTTP interface
- [ ] Integration test suite (end-to-end: ingest → materialize → query)
- [ ] Load test benchmarks: target ≥ 10k events/s on single `t3.large`
- [ ] Optional dashboard template (separate repo: `tinyraven/dashboard-template`, Next.js + Recharts)

### Success Criteria
```bash
# Kafka connector in .datasource file
CONNECTOR = kafka
CONNECTOR_CONFIG = {
  "brokers": "kafka:9092",
  "topic": "events",
  "consumer_group": "tinyraven"
}

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
