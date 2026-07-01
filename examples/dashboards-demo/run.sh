#!/usr/bin/env bash
# Dashboards drop-in demo — proves a Tinybird "user-facing dashboards" project
# runs unchanged on TinyRaven. Local runtime: Apple `container` (macOS 15+).
# Prod uses docker + compose instead; only the container-start lines differ.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TR="${TR:-tr}"                       # path to built `tr` binary (go build ./cmd/tr)
CH_PW="${CH_PW:-trdemo}"
ADMIN_TOKEN="${TR_ADMIN_TOKEN:-demo-admin-token}"

echo "== 1. start ClickHouse + Redis (Apple container) =="
container rm -f tr-ch tr-redis 2>/dev/null || true
container run -d --name tr-ch \
  --env CLICKHOUSE_PASSWORD="$CH_PW" --env CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  docker.io/clickhouse/clickhouse-server:latest >/dev/null
container run -d --name tr-redis docker.io/redis:7-alpine >/dev/null

# Apple container assigns a fresh IP per run — read it back (no fixed localhost bridge)
sleep 4
CH_IP=$(container ls | awk '/tr-ch/{print $6}'    | cut -d/ -f1)
RD_IP=$(container ls | awk '/tr-redis/{print $6}' | cut -d/ -f1)
until curl -sf "http://$CH_IP:8123/ping" >/dev/null; do sleep 1; done

export TR_HTTP_ADDR=":8000"
export TR_CLICKHOUSE_HTTP="http://$CH_IP:8123"
export TR_CLICKHOUSE_NATIVE="$CH_IP:9000"
export TR_CLICKHOUSE_DB="tr_main"
export TR_CLICKHOUSE_USER="default"
export TR_CLICKHOUSE_PASSWORD="$CH_PW"
export TR_REDIS_ADDR="$RD_IP:6379"
export TR_ADMIN_TOKEN="$ADMIN_TOKEN"
export TR_PROJECT_DIR="$HERE"

# `tr deploy` cannot yet bootstrap its own target DB (native client pins to it) — pre-create.
curl -s "http://$CH_IP:8123/" -H "X-ClickHouse-User: default" -H "X-ClickHouse-Key: $CH_PW" \
  --data-binary "CREATE DATABASE IF NOT EXISTS tr_main" >/dev/null

echo "== 2. deploy the SAME .datasource/.pipe files Tinybird would consume =="
"$TR" deploy --project-dir "$HERE" --branch main

echo "== 3. serve =="
"$TR" serve & SERVE_PID=$!; trap 'kill $SERVE_PID 2>/dev/null' EXIT
until curl -sf http://localhost:8000/health >/dev/null; do sleep 1; done

echo "== 4. ingest (Tinybird-identical POST /v0/events) =="
curl -s -X POST "http://localhost:8000/v0/events?name=web_events" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  --data-binary @"$HERE/sample-events.ndjson"; echo
sleep 6   # gatherer flush: max(N rows, 5s)

echo "== 5. query the dashboard pipes =="
echo "-- top_pages (table widget) --"
curl -s "http://localhost:8000/v0/pipes/top_pages.json?limit=5" -H "Authorization: Bearer $ADMIN_TOKEN"
echo "-- views_over_time (chart widget, DateTime param) --"
curl -s "http://localhost:8000/v0/pipes/views_over_time.json" \
  --data-urlencode "start=2026-07-01 10:00:00" -G -H "Authorization: Bearer $ADMIN_TOKEN"
