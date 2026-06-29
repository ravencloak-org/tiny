# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

**Pre-code.** The repo currently contains only planning docs — no Go module, no source, no build/test tooling yet. The first coding task is bootstrapping Phase 1 (see below).

- `PROMPT.md` — canonical architecture decisions and constraints. Read this first. Do **not** re-litigate decisions marked final (Go language, `chi` router, `tr` binary name, API-first / no dashboard).
- `MILESTONE.md` — five development phases with deliverables and success criteria. Drives what to build next.

## What TinyRaven is

Open-source, self-hosted, drop-in alternative to **Tinybird**, written in **Go** on top of **OSS ClickHouse**. Goal is 100% Tinybird API parity — existing Tinybird client code works by changing only `TINYBIRD_HOST`. Single binary serves both the HTTP server and the `tr` CLI (subcommands).

Model: ScyllaDB → Cassandra. Same API surface, leaner Go internals, Apache 2.0.

## Non-negotiable constraints

- **Go only.** No Kotlin, Python, or JVM in TinyRaven core.
- **Router is `chi`** (`net/http` + `github.com/go-chi/chi/v5`). Not gin/echo/fiber.
- **Binary = `tr`, package = `tinyraven`.** Never use `tb` (collides with the Tinybird CLI).
- **ClickHouse OSS only** — no fork, no private build. Skip the packed-part / zero-copy optimizations.
- **No built-in dashboard** — API-first; users connect Metabase/Superset/Grafana to ClickHouse directly.
- **Tinybird API parity is the spec.** Every endpoint, file format, error code, and JSON shape must match Tinybird. When unsure, match Tinybird's behavior.

## Architecture (the big picture)

Data path: `POST /v0/events` → **Gatherer** (goroutine + channel, batches on `max(N events, 5s)`) → ClickHouse. Query path: `GET /v0/pipes/{name}.json` → parse `{{Type(name, default)}}` template → validate/escape params → ClickHouse HTTP → `FORMAT JSONEachRow`.

Three backing stores, each with a distinct job:
- **ClickHouse** — event data, materialized views, query execution, `tinybird.pipe_stats` observability table.
- **Redis** — the metadata registry (datasource + pipe definitions, tokens, deploy state) **and** the hot cache (token validation, rate-limit counters, query results). Runs AOF-persisted as a system of record. No PostgreSQL — see `docs/adr/0001-redis-only-metadata.md`. Git (`.datasource`/`.pipe` files) is the source of truth for definitions; the registry is rebuildable via `tr deploy`.

Branching = one ClickHouse database per git branch (`workspace_{branch}`); `tr deploy` detects the current branch and targets the matching DB. Breaking migrations use shadow table → MV backfill → atomic `EXCHANGE TABLES`.

Project files the tool consumes: `.datasource` (SCHEMA + ENGINE config) and `.pipe` (NODE / ENDPOINT / MATERIALIZATION blocks with `{{Type(param)}}` SQL templates). Formats are defined in PROMPT.md and must stay byte-compatible with Tinybird.

### Intended Go layout

Target package structure is specified in PROMPT.md ("Go Project Structure"): `cmd/tr/` (CLI entrypoint + subcommands), `internal/api/` (chi server, handlers, middleware), and `internal/{gatherer,pipe,datasource,clickhouse,auth}/` for the core subsystems. Follow it when scaffolding rather than inventing a new structure.

## Build & test

No tooling exists yet. Once the Go module is initialized, standard Go applies (`go build ./...`, `go test ./...`, `go test -run TestName ./pkg`). Add concrete commands to this section as they land. Use **`bun`/`bunx`** for any JS tooling (never npm/npx).
