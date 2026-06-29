<div align="center">

# TinyRaven

**Open-source, self-hosted, drop-in alternative to [Tinybird](https://www.tinybird.co/).**

Built in Go on top of OSS ClickHouse. Same API. Your infrastructure.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Status: Pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange.svg)](MILESTONE.md)
[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://go.dev)

</div>

> **Status: pre-alpha — actively building Phase 1.** No release yet. The commands and install methods below describe the target developer experience; track real progress in [MILESTONE.md](MILESTONE.md) and the [issues](https://github.com/ravencloak-org/tiny/issues).

---

## What is TinyRaven?

TinyRaven replicates Tinybird's full developer experience — HTTP ingestion, SQL pipes, REST endpoint publishing, a CLI, branching, and git workflows — but runs entirely on infrastructure you own. It speaks the **exact same API** as Tinybird, so existing client code works by changing one environment variable.

The model is **ScyllaDB → Cassandra**: identical API surface, leaner internals, fully open. Tinybird runs a private ClickHouse fork behind a Python/C++ stack; TinyRaven is a single Go binary in front of stock open-source ClickHouse.

- **Backend + CLI:** one Go binary (`tr`), `net/http` + `chi`
- **Database:** OSS ClickHouse (Apache 2.0), target **26.3 LTS**
- **Metadata + cache:** Redis (AOF-persisted; system of record for metadata)
- **License:** Apache 2.0 — free and feature-complete forever, no paywall, no gated "enterprise" features ([ADR 0021](docs/adr/0021-monetization-sustainability-only.md))

## Drop-in compatibility

Point your existing Tinybird project at TinyRaven by changing the host:

```bash
export TINYBIRD_HOST=https://tinyraven.example.com
export TINYBIRD_TOKEN=your_token
tr deploy        # same .datasource / .pipe files, your backend
```

### API parity

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/v0/events` | POST | JSON / NDJSON event ingestion |
| `/v0/pipes/{name}.json` | GET | Parameterized SQL query execution |
| `/v0/sql` | GET | Direct read-only ClickHouse SQL proxy |
| `/v0/metrics` | GET | Prometheus-format metrics |
| `/health` | GET | Health check |

`.datasource` and `.pipe` files use Tinybird's exact format, including `{{Type(param, default)}}` SQL templating.

## Architecture

`POST /v0/events` batches through an in-process **Gatherer** into ClickHouse; `GET /v0/pipes/{name}.json` parses the `{{Type(...)}}` template, binds params as ClickHouse query parameters, and streams the result. **ClickHouse** holds event data, materialized views, and query execution; **Redis** holds the metadata registry (datasource/pipe definitions, tokens, deploy state) plus hot cache, AOF-persisted as a system of record. Branching = one ClickHouse database per git branch (`tr_{branch}`).

Full data flow, the deps table, and every locked decision live in **[PROMPT.md](PROMPT.md)** and the [ADRs](docs/adr/) — the spec, not duplicated here.

## Quickstart (planned)

```bash
brew install tinyraven        # installs the `tr` binary
tr local start                # ClickHouse + TinyRaven + Redis via Docker Compose

# ingest
curl -X POST "http://localhost:8000/v0/events?name=events" \
  -H "Authorization: Bearer $TR_TOKEN" \
  -d '{"user_id":"alice","event":"page_view"}'

# query a published pipe
curl "http://localhost:8000/v0/pipes/user_metrics.json?user_id=alice" \
  -H "Authorization: Bearer $TR_TOKEN"
```

## Install (planned)

| Method | Command |
|--------|---------|
| Homebrew | `brew install tinyraven` |
| APT | `sudo apt install tinyraven` |
| SDKMAN | `sdk install tinyraven` |
| Docker | `docker run -p 8000:8000 ghcr.io/tinyraven/tinyraven:latest` |

> Package name is always `tinyraven`; the binary is always `tr`. We never use `tb` (the Tinybird CLI) to avoid conflicts.

## Roadmap

| Phase | Deliverable | Gate |
|-------|-------------|------|
| **1 — MVP** | `tr local start` + events + pipes | POST → GET round-trip works |
| **2 — API** | `tr deploy` + OpenAPI + metrics | Full deploy + query cycle |
| **3 — Workflows** | Branches + materialized views | Zero-downtime migration |
| **4 — Distribution** | Brew / APT / Heroku / AWS install | One-click deploy everywhere |
| **5 — Community** | Connectors + BI integrations | 10k events/s benchmark |

Full breakdown in [MILESTONE.md](MILESTONE.md). Architecture decisions live in [PROMPT.md](PROMPT.md).

## What TinyRaven is not

- **No built-in dashboard** — API-first. Connect Metabase, Superset, or Grafana straight to ClickHouse.
- **No managed cloud** — pure self-hosted FOSS.
- **No ClickHouse fork** — stock OSS ClickHouse, as-is.

## Contributing

Pre-alpha — the codebase is being bootstrapped. The best way to help right now is to browse the [issues](https://github.com/ravencloak-org/tiny/issues), grouped by phase milestone, and pick up a Phase 1 task.

## License

[Apache 2.0](LICENSE).

---

## Deploy & Install

> Distribution targets land in **Phase 4**. The one-liners and buttons below describe the intended experience; release artifacts publish on the first tagged release (`git tag vX.Y.Z`).

### Install the `tr` binary

```bash
# macOS / Linux — Homebrew
brew tap ravencloak-org/tinyraven
brew install tinyraven            # installs the `tr` binary

# Debian / Ubuntu — .deb from GitHub Releases
sudo apt install ./tinyraven_*_linux_amd64.deb

# RHEL / Fedora — .rpm from GitHub Releases
sudo rpm -i tinyraven_*_linux_amd64.rpm

# Docker
docker run -p 8000:8000 ghcr.io/ravencloak-org/tiny:latest serve
```

Binaries for Linux/macOS/Windows × amd64/arm64, plus `.deb` / `.rpm` packages
and SHA256 checksums, are built by [GoReleaser](.goreleaser.yaml) on every tag.

### One-click cloud deploy

[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https://github.com/ravencloak-org/tiny)
[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new?repo=https://github.com/ravencloak-org/tiny)
[![Launch on AWS](https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png)](https://console.aws.amazon.com/cloudformation/home#/stacks/new?stackName=tinyraven&templateURL=https://raw.githubusercontent.com/ravencloak-org/tiny/main/cloudformation/tinyraven-template.yaml)
[![Deploy to DigitalOcean](https://www.deploytodo.com/do-btn-blue.svg)](https://cloud.digitalocean.com/apps/new?repo=https://github.com/ravencloak-org/tiny)

### Kubernetes (Helm)

```bash
helm install tinyraven ./charts/tinyraven \
  --set env.clickhouse.http=http://clickhouse:8123 \
  --set env.redis.addr=redis:6379
```

### Per-platform guides

| Platform | Guide |
|----------|-------|
| Docker | [docs/deploy/docker.md](docs/deploy/docker.md) |
| Kubernetes / Helm | [docs/deploy/kubernetes.md](docs/deploy/kubernetes.md) |
| Heroku | [docs/deploy/heroku.md](docs/deploy/heroku.md) |
| AWS (CloudFormation) | [docs/deploy/aws.md](docs/deploy/aws.md) |
| Railway | [docs/deploy/railway.md](docs/deploy/railway.md) |
| Dokploy + Cloudflare | [docs/deploy/dokploy.md](docs/deploy/dokploy.md) |

Coming from Tinybird? See [docs/migrate-from-tinybird.md](docs/migrate-from-tinybird.md) — install `tr`, point `TINYBIRD_HOST`, `tr deploy`.

Every target needs an external **ClickHouse 26.3** and **Redis** — TinyRaven's `tr` server is stateless.
