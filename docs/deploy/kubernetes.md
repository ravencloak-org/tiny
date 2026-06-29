# Deploy TinyRaven on Kubernetes (Helm)

The chart in [`charts/tinyraven`](../../charts/tinyraven) deploys the stateless
`tr` server. **ClickHouse and Redis are external** — the chart does not provision
them. Point the values at your running ClickHouse 26.3 and Redis.

## Install

```bash
helm install tinyraven ./charts/tinyraven \
  --set env.clickhouse.http=http://clickhouse:8123 \
  --set env.clickhouse.native=clickhouse:9000 \
  --set env.redis.addr=redis:6379 \
  --set adminToken=$(openssl rand -hex 24)
```

Or with a values file:

```bash
helm install tinyraven ./charts/tinyraven -f my-values.yaml
```

## Key values

| Key | Default | Purpose |
|-----|---------|---------|
| `replicaCount` | `1` | Number of `tr` pods (single-node design — see ADR 0031) |
| `image.repository` | `ghcr.io/ravencloak-org/tiny` | Image |
| `image.tag` | `latest` | Image tag |
| `service.port` | `8000` | Service port |
| `env.clickhouse.http` | `http://clickhouse:8123` | ClickHouse HTTP |
| `env.clickhouse.native` | `clickhouse:9000` | ClickHouse native TCP |
| `env.redis.addr` | `redis:6379` | Redis |
| `adminToken` | `change-me` | Bootstrap admin token (creates a Secret) |
| `existingSecret` | `""` | Use a Secret you manage instead of `adminToken` |
| `ingress.enabled` | `false` | Expose via Ingress |

## Secrets

By default the chart creates a Secret holding `TR_ADMIN_TOKEN` (and the optional
`TR_CLICKHOUSE_PASSWORD`). To manage it yourself, create a Secret with those keys
and set `existingSecret=<name>`.

## Probes

Liveness hits `/health`; readiness hits `/health/ready` (verifies Redis +
ClickHouse). If readiness stays red, your `env.clickhouse.*` / `env.redis.addr`
are likely wrong.

## Verify

```bash
kubectl port-forward svc/tinyraven 8000:8000
curl localhost:8000/health
```

## Uninstall

```bash
helm uninstall tinyraven
```
