# Connecting BI tools (Metabase, Superset, Grafana, DBeaver)

TinyRaven stores everything in **plain OSS ClickHouse**. There is no proprietary
format and no TinyRaven-specific driver — so every BI tool that speaks ClickHouse
connects **directly to ClickHouse**, with zero extra work on TinyRaven's side.
This is by design: TinyRaven is API-first and ships no dashboard (see the
"No built-in dashboard" scope limit in `PROMPT.md`).

BI tools connect to ClickHouse, **not** to the `tr` HTTP server. The `tr` server
owns ingestion (`POST /v0/events`) and the parameterised pipe/`/v0/sql` API;
read-only analytics and dashboards go straight to the database.

## Connection facts

| Setting | Value | Notes |
|---------|-------|-------|
| Host | your ClickHouse host | e.g. `localhost`, or the `clickhouse` service in `docker-compose.yml` |
| HTTP port | `8123` | used by Metabase, Superset, Grafana, DBeaver (HTTP) |
| Native (TCP) port | `9000` | used by DBeaver's native driver and `clickhouse-client` |
| Database | `tr_main` | one DB per git branch — a feature branch is `tr_<branch>` (ADR 0007) |
| User | a **read-only** ClickHouse user (see below) | |
| TLS | off locally; on in production | put ClickHouse behind TLS for remote BI access |

## Create a dedicated read-only user (do this first)

TinyRaven's `/v0/sql` endpoint enforces `readonly=2` per query (ADR 0011), but a
BI tool bypasses that endpoint and talks to ClickHouse directly. So give BI tools
their own read-only ClickHouse user with resource caps. Run once as an admin:

```sql
CREATE USER IF NOT EXISTS bi IDENTIFIED BY 'change-me'
  SETTINGS readonly = 2,
           max_execution_time = 30,
           max_result_rows = 1000000,
           max_memory_usage = 5000000000;

GRANT SELECT, SHOW ON tr_main.* TO bi;   -- repeat per branch DB you expose
```

`readonly = 2` structurally refuses writes, DDL, and settings changes — there is
no SQL parsing to outsmart. The caps stop a single dashboard query from
exhausting the node.

## Metabase

1. **Admin → Databases → Add database**, type **ClickHouse** (install the
   ClickHouse driver plugin if it isn't listed).
2. Host = your CH host, Port = `8123`, Database = `tr_main`.
3. Username = `bi`, password = the one you set above.
4. Save and sync. Tables and materialized views appear as queryable models.

## Apache Superset

1. Install the driver in the Superset image:
   `pip install clickhouse-connect`.
2. **Settings → Database Connections → + Database → ClickHouse Connect**.
3. SQLAlchemy URI:
   ```
   clickhousedb://bi:change-me@your-host:8123/tr_main
   ```
   (use `clickhousedbs://` for TLS).
4. Test connection, then build datasets on the TinyRaven tables.

## Grafana

1. Install the official **ClickHouse data source** plugin
   (`grafana-clickhouse-datasource`).
2. **Connections → Add data source → ClickHouse**.
3. Server address = your CH host, Port = `8123`, Protocol = **HTTP**
   (or `9000` / **Native**), Database = `tr_main`.
4. Username = `bi`, password as above. Save & test, then chart your pipes'
   underlying tables. Time-series panels work directly off `DateTime` columns.

## DBeaver

1. **Database → New Database Connection → ClickHouse**.
2. Host = your CH host, Port = `8123` (HTTP) or `9000` (native), Database =
   `tr_main`.
3. User = `bi`, password as above. DBeaver downloads the ClickHouse JDBC driver
   automatically on first connect.
4. Browse schemas and run ad-hoc `SELECT`s for exploration.

## Branches

Each git branch deploys to its own ClickHouse database (`tr_<branch>`; ADR 0007).
To point a BI tool at a branch's data, change the **Database** field to that
branch's DB name and grant the `bi` user `SELECT` on it. Production dashboards
should pin `tr_main`.
