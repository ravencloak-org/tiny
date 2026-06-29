# Deploy TinyRaven with Docker

The fastest way to run TinyRaven. The `tr` server is stateless — it needs an
external **ClickHouse 26.3** and **Redis** (AOF on). The published image is
`ghcr.io/ravencloak-org/tiny:latest` (public, linux/amd64).

## Option A — Compose (bundles ClickHouse + Redis)

The repo ships a production compose file that brings up all three services with
persistent volumes and health gating:

```bash
git clone https://github.com/ravencloak-org/tiny
cd tiny
cp .env.example .env          # edit TR_ADMIN_TOKEN at minimum
docker compose -f deploy/docker-compose.prod.yml up -d
```

Then deploy your project files and query:

```bash
tr deploy
curl localhost:8000/health
```

## Option B — Image only (bring your own ClickHouse + Redis)

```bash
docker run -p 8000:8000 \
  -e TR_CLICKHOUSE_HTTP=http://clickhouse:8123 \
  -e TR_CLICKHOUSE_NATIVE=clickhouse:9000 \
  -e TR_CLICKHOUSE_DB=tr_main \
  -e TR_REDIS_ADDR=redis:6379 \
  -e TR_ADMIN_TOKEN=change-me \
  -v "$PWD":/project \
  ghcr.io/ravencloak-org/tiny:latest serve
```

The container runs `tr serve` on port **8000**. Mount your `.datasource` /
`.pipe` files at `TR_PROJECT_DIR` (default `/project`).

## Environment

| Var | Purpose | Example |
|-----|---------|---------|
| `TR_HTTP_ADDR` | Bind address | `:8000` |
| `TR_CLICKHOUSE_HTTP` | ClickHouse HTTP (queries) | `http://clickhouse:8123` |
| `TR_CLICKHOUSE_NATIVE` | ClickHouse native TCP (inserts) | `clickhouse:9000` |
| `TR_CLICKHOUSE_DB` | Database | `tr_main` |
| `TR_CLICKHOUSE_USER` / `TR_CLICKHOUSE_PASSWORD` | CH auth | `default` / `` |
| `TR_REDIS_ADDR` | Redis (metadata + cache) | `redis:6379` |
| `TR_ADMIN_TOKEN` | Bootstrap admin token | a strong secret |
| `TR_PROJECT_DIR` | `.datasource`/`.pipe` location | `/project` |

## Health

- `GET /health` — liveness
- `GET /health/ready` — readiness (checks Redis + ClickHouse)

See the full list in [`.env.example`](../../.env.example).
