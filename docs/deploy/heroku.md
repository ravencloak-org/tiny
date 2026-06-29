# Deploy TinyRaven to Heroku

One-click deploy via the [`app.json`](../../app.json) in the repo root. It uses
the `heroku/go` buildpack and provisions **heroku-redis:mini**. ClickHouse is
external (no Postgres — see [ADR 0001](../adr/0001-redis-only-metadata.md)).

## One-click

[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https://github.com/ravencloak-org/tiny)

Click the button, fill in the ClickHouse env vars, and deploy (~5 minutes).

## CLI

```bash
heroku create my-tinyraven
heroku addons:create heroku-redis:mini
heroku buildpacks:set heroku/go
git push heroku main
```

## Required config

Set these (the button prompts for them):

```bash
heroku config:set \
  TR_CLICKHOUSE_HTTP=https://abc.clickhouse.cloud:8443 \
  TR_CLICKHOUSE_NATIVE=abc.clickhouse.cloud:9440 \
  TR_CLICKHOUSE_DB=tr_main \
  TR_ADMIN_TOKEN=$(openssl rand -hex 24)
```

## Wiring Redis

The `heroku-redis` add-on exposes its connection string as `REDIS_URL`
(`redis://...`), but TinyRaven reads a plain `host:port` from `TR_REDIS_ADDR`.
Map it once after the add-on is attached:

```bash
# Strip the scheme/credentials from REDIS_URL into host:port.
REDIS_URL=$(heroku config:get REDIS_URL)
HOSTPORT=${REDIS_URL#*@}
heroku config:set TR_REDIS_ADDR="$HOSTPORT"
```

(If your Redis requires TLS/auth, use a managed Redis that gives you a stable
`host:port` plus credentials and set them accordingly.)

## Port binding

Heroku injects `$PORT`. `app.json` sets `TR_HTTP_ADDR=:$PORT` so `tr serve`
binds the router-assigned port.

## Verify

```bash
heroku open                 # opens /health (success_url)
curl https://my-tinyraven.herokuapp.com/health
```
