# Deploy TinyRaven to Railway

[`railway.json`](../../railway.json) builds from the repo `Dockerfile` and runs
`tr serve` with a `/health/ready` healthcheck. Add a Redis plugin in the project;
ClickHouse is external (ClickHouse Cloud or your own host).

## One-click

[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new?repo=https://github.com/ravencloak-org/tiny)

## Steps

1. Click the button (or `railway init` from a clone) and pick this repo.
2. In the project, **+ New → Database → Redis**. Railway exposes
   `REDIS_HOST` / `REDIS_PORT` (or a `REDIS_URL`) as service variables.
3. Set the service variables on the `tr` service:

   ```
   TR_HTTP_ADDR        = :8000
   TR_CLICKHOUSE_HTTP  = https://abc.clickhouse.cloud:8443
   TR_CLICKHOUSE_NATIVE= abc.clickhouse.cloud:9440
   TR_CLICKHOUSE_DB    = tr_main
   TR_CLICKHOUSE_PASSWORD = <secret>
   TR_REDIS_ADDR       = ${{Redis.REDIS_HOST}}:${{Redis.REDIS_PORT}}
   TR_ADMIN_TOKEN      = <strong-secret>
   TR_PROJECT_DIR      = /project
   ```

   (`${{Redis.*}}` are Railway variable references to the Redis plugin.)
4. Deploy. Railway exposes the service on a public domain in ~5 minutes.

## Verify

```bash
curl https://<your-app>.up.railway.app/health
```

The deploy is healthy once `/health/ready` passes (Redis + ClickHouse reachable).
