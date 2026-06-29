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

```
                    POST /v0/events
                          │
                          ▼
                  ┌───────────────┐   flush on max(N events, 5s)
   clients ──────▶│   Gatherer    │──────────────────────────────┐
                  │ (goroutine +  │                               ▼
                  │   channel)    │                        ┌─────────────┐
                  └───────────────┘                        │  ClickHouse │
                                                           │   (OSS)     │
   GET /v0/pipes/{name}.json                               └─────────────┘
            │                                                     ▲
            ▼                                                     │
   parse {{Type(...)}} ─▶ validate/escape ─▶ ClickHouse HTTP ─────┘
                                              FORMAT JSON / JSONEachRow
```

| Store | Role |
|-------|------|
| **ClickHouse** | Event data, materialized views, query execution, `tinybird.pipe_stats` |
| **Redis** | Metadata registry (datasource + pipe definitions, tokens, deploy state) **and** hot cache / rate-limit counters. Runs AOF-persisted as a system of record. |

**Branching:** one ClickHouse database per git branch (`tr_{branch}`). `tr deploy` detects the current branch and targets the matching database. Breaking migrations use shadow table → MV backfill → atomic `EXCHANGE TABLES`.

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
