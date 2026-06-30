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
TR_CLICKHOUSE_DB=tr_main           # optional
TR_PORT=18000                      # optional host port (default 18000; avoid 80/8000/8080)
```

The `tinyraven`/`site` image tags come from `TINYRAVEN_TAG`/`SITE_TAG` (default
`latest`) with `pull_policy: always`. You don't set them by hand — the release
pipeline pins them to each version tag for you (§4). Updates are automatic.

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

## 4. Auto-deploy on release (tag → prod, fully automatic)

Once wired, **pushing a `v*` tag updates prod with no manual step** — no SSH, no
UI click:

1. `git tag vX.Y.Z && git push --tags`
2. `.github/workflows/release.yml` builds + pushes the version-tagged images
   `ghcr.io/ravencloak-org/{tiny,tiny-site}:vX.Y.Z` (and `:latest`).
3. Its `deploy-dokploy` job pins the Dokploy compose env to that tag through the
   Dokploy API: `POST /api/compose.update` rewrites the env blob with
   `TINYRAVEN_TAG=vX.Y.Z` / `SITE_TAG=vX.Y.Z`, then `POST /api/compose.deploy`
   redeploys.
4. Because each release is a **new image reference** (a version tag, not the
   moving `:latest`), Dokploy always pulls the new digest. The old deploy-webhook
   + `:latest` path recreated the stack from cache and never updated — pinning
   the version tag is what guarantees the freshly-built image actually goes live.

**One-time setup** — add two repo secrets (Settings → Secrets and variables →
Actions); the Dokploy URL and compose ID are already baked into the workflow:

| Secret | Purpose |
| --- | --- |
| `DOKPLOY_TOKEN` | Dokploy API key (`x-api-key`) for `compose.update` + `compose.deploy`. |
| `TR_ADMIN_TOKEN` | Bootstrap admin token. `compose.update` **replaces** the whole env blob, so it is re-supplied on every deploy. |

Without `DOKPLOY_TOKEN` the deploy step no-ops (CI stays green) and you redeploy
from the Dokploy UI button instead. Note that `compose.update` overwrites the
compose environment wholesale, so any env you set by hand in the Dokploy UI is
dropped on the next tag deploy — keep CI-managed env (the admin token, the image
tags) in the workflow, not the UI.

## 5. Expose at tiny.ravencloak.org (Cloudflare Tunnel)

> **404 troubleshooting:** a 404 at `tiny.ravencloak.org` means the request
> reached Cloudflare but **no tunnel ingress rule matches that hostname** — the
> tunnel isn't running or has no route to the service. A hand-made CNAME is NOT
> enough: a token/remotely-managed tunnel manages its own DNS and routes are set
> by *Public Hostname* (or DockFlare labels), not a manual record. Also, when
> cloudflared runs in the compose, the service URL is the **internal**
> `http://tinyraven:8000`, never the host's `:18000`.

### Option A — cloudflared in the compose (recommended, simplest)

The prod compose ships a `cloudflared` service behind the `tunnel` profile. In
Cloudflare Zero Trust → Networks → Tunnels, create a tunnel, copy its **token**,
then in Dokploy set:

```
CF_TUNNEL_TOKEN=<tunnel token>
COMPOSE_PROFILES=tunnel
```

Redeploy. Then in the same tunnel add a **Public Hostname**:
`tiny.ravencloak.org` → Type **HTTP** → URL **`http://tinyraven:8000`**. That
auto-creates the DNS record (delete any manual CNAME) and fixes the 404.

### Option B — standalone cloudflared (config file)

`deploy/cloudflared/config.example.yml` + `cloudflared tunnel create/route/run`
on the host. Point `ingress[0].service` at `http://localhost:18000` (the
published host port) or the container.

### Option C — DockFlare (label-driven, host-wide)

[DockFlare](https://github.com/ChrispyBacon-dev/DockFlare) auto-manages tunnel
ingress + DNS from Docker labels — no dashboard step, and one tunnel for *every*
service on the host. Deploy its upstream compose as a **separate** Dokploy stack
(needs a CF API token + account/zone IDs), put it + this stack on a **shared
external docker network**, then uncomment the `dockflare.*` labels on the
`tinyraven` service in `docker-compose.prod.yml` (`dockflare.hostname=
tiny.ravencloak.org`, `dockflare.service=http://tinyraven:8000`). DockFlare
discovers the label and wires the route + DNS automatically. Best if you want all
your host services (viewrr, caw, …) behind one label-driven tunnel.

## 6. Verify

```bash
curl https://tiny.ravencloak.org/health           # {"status":"ok"}
curl https://tiny.ravencloak.org/health/ready      # {"status":"ready"} once CH+Redis reachable
# Tinybird clients: point TINYBIRD_HOST=https://tiny.ravencloak.org
```
