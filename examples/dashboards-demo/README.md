# Dashboards drop-in demo

Proves Tinybird's flagship **user-facing dashboards** use case
(<https://www.tinybird.co/docs/use-cases>) runs on TinyRaven with the *same
files and the same HTTP calls* — only `TINYBIRD_HOST` changes.

A dashboard is two widgets over one event stream:

| File | Widget | TinyRaven feature exercised |
|------|--------|-----------------------------|
| `web_events.datasource` | — | `POST /v0/events` ingestion → MergeTree |
| `top_pages.pipe` | table | parameterized pipe, `{{Int32(limit,10)}}` |
| `views_over_time.pipe` | time-series chart | `{{DateTime(start,...)}}` param filter |

These are byte-for-byte Tinybird `.datasource` / `.pipe` format.

## Run (local, Apple `container`)

```bash
go build -o /tmp/tr ./cmd/tr
TR=/tmp/tr ./examples/dashboards-demo/run.sh
```

Local runtime is Apple `container` (macOS). On the prod server swap the two
`container run` lines for `docker compose` — everything else is identical.

## Verified output (2026-07-01, ClickHouse 26.6, TinyRaven v0.3.2)

Ingest — Tinybird-identical response shape:

```json
{"quarantined_rows":0,"successful_rows":300}
```

`GET /v0/pipes/top_pages.json?limit=5` — full Tinybird JSON envelope
(`meta` / `data` / `rows` / `rows_before_limit_at_least` / `statistics`):

```json
{"data":[
  {"path":"/blog/launch","views":82,"visitors":49},
  {"path":"/","views":78,"visitors":52},
  {"path":"/docs","views":46,"visitors":35},
  {"path":"/features","views":42,"visitors":34},
  {"path":"/pricing","views":42,"visitors":29}],
 "rows":5,"rows_before_limit_at_least":6}
```

`GET /v0/pipes/views_over_time.json?start=2026-07-01 10:00:00` — DateTime param
filters the series (buckets before 10:00 dropped):

```json
{"data":[
  {"hour":"2026-07-01 10:00:00","views":43},
  {"hour":"2026-07-01 11:00:00","views":64},
  {"hour":"2026-07-01 12:00:00","views":50},
  {"hour":"2026-07-01 13:00:00","views":19}],
 "rows":4}
```

Auth parity: no token → `401`; wrong scope → `403` (token scopes `ADMIN`,
`READ:<pipe>`, `APPEND:<datasource>`).

## Known rough edge (surfaced by this demo)

`tr deploy` cannot bootstrap its own target database — the native ClickHouse
client pins the session to `TR_CLICKHOUSE_DB`, so `CREATE DATABASE tr_main`
fails with `UNKNOWN_DATABASE` when the DB is absent. `run.sh` pre-creates it.
Fix = open the bootstrap connection against `default`. Worth an issue.
