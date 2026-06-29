# Deploy TinyRaven to Dokploy + Cloudflare Tunnel

Deploys the `tr` server to a [Dokploy](https://dokploy.com) instance and exposes
it at `tiny.ravencloak.org` via a Cloudflare Tunnel. TinyRaven is stateless — you
bring ClickHouse + Redis.

## 1. Backing stores

TinyRaven needs an external **ClickHouse 26.3** and **Redis** (AOF on). Either
run them as Dokploy services/databases, or point at managed instances. Note their
internal addresses for the env vars below.

## 2. Create the app in Dokploy

### Option A — One-click Docker Compose (recommended: bundles CH + Redis)

Dokploy → Create → **Docker Compose**, connect the repo `ravencloak-org/tiny`,
compose path `deploy/docker-compose.prod.yml`. It brings up ClickHouse + Redis +
TinyRaven together (persistent volumes, health-gated). Set env in Dokploy:

```
TR_ADMIN_TOKEN=<strong-secret>     # required
TINYRAVEN_TAG=v0.1.1               # or latest
TR_CLICKHOUSE_DB=tr_main           # optional
TR_PORT=18000                      # optional host port (default 18000; avoid 80/8000/8080)
```

The image ships the `examples/quickstart` datasource + pipe at `/project`, so the
app is queryable immediately; replace `/project` (volume or your own image) with
your real `.datasource`/`.pipe` files.

### Option B — Image only (you bring CH + Redis)

Source = Docker image `ghcr.io/ravencloak-org/tiny:v0.1.1`, command `serve`, port
**8000**, env from `.env.example` pointing at your own ClickHouse + Redis. (Or a
Dockerfile build from the repo.)

Container port: **8000**. The `ghcr.io/ravencloak-org/tiny` package is public.

## 3. Environment

Set these in Dokploy → Environment (see `.env.example`):

```
TR_HTTP_ADDR=:8000
TR_CLICKHOUSE_HTTP=http://<clickhouse-host>:8123
TR_CLICKHOUSE_NATIVE=<clickhouse-host>:9000
TR_CLICKHOUSE_DB=tr_main
TR_CLICKHOUSE_USER=default
TR_CLICKHOUSE_PASSWORD=<secret>
TR_REDIS_ADDR=<redis-host>:6379
TR_PROJECT_DIR=/project
TR_ADMIN_TOKEN=<strong-secret>   # bootstrap admin token — keep secret
```

Health checks: liveness `GET /health`, readiness `GET /health/ready`.

## 4. Auto-deploy on release (tag → Dokploy)

The `.github/workflows/release.yml` job `deploy-dokploy` POSTs to your app's
Dokploy **Deploy Webhook** on every `v*` tag.

1. In Dokploy: app → **Deployments → Webhook**, copy the deploy webhook URL.
2. In GitHub: repo → Settings → Secrets and variables → Actions → add secret
   **`DOKPLOY_DEPLOY_WEBHOOK`** = that URL.

Without the secret the step no-ops (CI stays green).

## 5. Cloudflare Tunnel → tiny.ravencloak.org

Use `deploy/cloudflared/config.example.yml`. On a host with `cloudflared`
installed and authenticated (`cloudflared login` against the ravencloak.org zone):

```bash
cloudflared tunnel create tinyraven
cloudflared tunnel route dns tinyraven tiny.ravencloak.org
# fill TUNNEL_ID + credentials path + the `service:` target (Dokploy app addr)
cloudflared tunnel --config deploy/cloudflared/config.yml run tinyraven
```

Run the tunnel as a Dokploy service / systemd unit alongside the app. Point
`ingress[0].service` at the app's internal address (Dokploy service name `:8000`,
or the Dokploy Traefik proxy if you route by Host header).

## 6. Verify

```bash
curl https://tiny.ravencloak.org/health           # {"status":"ok"}
curl https://tiny.ravencloak.org/health/ready      # {"status":"ready"} once CH+Redis reachable
# Tinybird clients: point TINYBIRD_HOST=https://tiny.ravencloak.org
```
