# Migrate from Tinybird to TinyRaven

TinyRaven speaks the **exact same API** as Tinybird, so migration is a host
swap, not a rewrite. Your existing `.datasource` / `.pipe` files, client SDK
calls, and CI deploy steps keep working — you change where they point.

## The 3-step path

### 1. Install `tr`

```bash
# macOS / Linux (Homebrew)
brew tap ravencloak-org/tinyraven
brew install tinyraven

# Debian / Ubuntu
sudo apt install ./tinyraven_*_linux_amd64.deb   # from GitHub Releases

# Docker
docker run ghcr.io/ravencloak-org/tiny:latest tr --version
```

The binary is always `tr` (never `tb` — that's the Tinybird CLI, and the two
coexist without conflict).

### 2. Point `TINYBIRD_HOST` at your TinyRaven

TinyRaven honours the standard Tinybird env vars and `~/.tinyraven/config.yml`
(same format as `~/.tinybird/config.yml`):

```bash
export TINYBIRD_HOST=https://tinyraven.example.com
export TINYBIRD_TOKEN=your_tinyraven_token
```

Your application code changes by exactly one variable — the `TINYBIRD_HOST` it
already reads. Ingestion (`POST /v0/events`) and queries
(`GET /v0/pipes/{name}.json`) hit the same paths with the same JSON shapes and
error codes.

### 3. Deploy your existing project

From the same repo that holds your `.datasource` and `.pipe` files:

```bash
tr deploy
# ✓ Validated N datasources, M pipes
# ✓ Published M endpoints
```

That's it — same files, your infrastructure.

## What carries over unchanged

- `.datasource` files (SCHEMA + ENGINE config)
- `.pipe` files (NODE / ENDPOINT / MATERIALIZATION blocks, `{{Type(param, default)}}` templates)
- The `/v0/events`, `/v0/pipes/{name}.json`, and `/v0/sql` endpoints
- Tinybird-compatible error codes and JSON error shapes
- Git-based workflow: one ClickHouse database per branch (`tr_{branch}`)

## What's different

- **You run the backend.** TinyRaven needs an external **ClickHouse 26.3** and
  **Redis** — see the [deploy guides](deploy/). Redis holds the metadata
  registry and cache; there is no Postgres ([ADR 0001](adr/0001-redis-only-metadata.md)).
- **No managed cloud / no built-in dashboard.** Connect Metabase, Superset, or
  Grafana directly to ClickHouse.
- **Stock OSS ClickHouse** — no private fork, so the packed-part / zero-copy
  optimizations from Tinybird's hosted product don't apply.

## Moving historical data

Export from Tinybird and bulk-load into your ClickHouse, or re-point your
ingestion source at TinyRaven and backfill from the original event source. Since
both sit on ClickHouse, `clickhouse-client` / `INSERT ... SELECT` from an
`s3()` / `url()` source is the fastest path for large historical loads.
