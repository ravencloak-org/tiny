# Connector templates

Working `.datasource` / `.pipe` templates for the three connector engines
TinyRaven supports: **Kafka**, **S3**, and **PostgreSQL**.

## The model: connectors are ClickHouse engines, not built services

TinyRaven does **not** run Kafka consumers, S3 pollers, or CDC schedulers. A
connector is just a `.datasource` that declares a ClickHouse-native engine. `tr
deploy` creates the ClickHouse objects; **ClickHouse does the actual pulling.**

This is the whole of [ADR 0019](../../docs/adr/0019-connectors-via-clickhouse-engines.md).
The trade-off, stated loudly: Tinybird's connectors are *managed* (its infra runs
the consumers, offsets, dead-letter handling, and a Connect UI). TinyRaven's are
*CH-native* — you declare and operate the engine. The same data lands; the
operational surface differs. A drop-in user migrating a Kafka pipeline declares a
ClickHouse Kafka engine instead of clicking "Connect".

The HTTP Events API (`POST /v0/events` → Gatherer) remains the only
TinyRaven-owned ingestion path. Connector engines are an additional, CH-driven
path — they do **not** flow through the Gatherer, so they have no `_quarantine`
sibling table.

## What's here

| File | Engine | Purpose |
|------|--------|---------|
| `kafka_events.datasource` | `Kafka` | Consumer table — ClickHouse runs the consumer group |
| `events_mt.datasource` | `MergeTree` | Durable target the Kafka MV writes into |
| `kafka_to_events.pipe` | materialization | MV that drains `kafka_events` → `events_mt` |
| `s3_import.datasource` | `S3` | Query objects in S3 as a table |
| `postgres_users.datasource` | `PostgreSQL` | Query a live Postgres table through ClickHouse |

## Kafka is two objects, not one

A Kafka engine table is a **stream**: reading a row consumes it. You almost never
query it directly. The pattern is a **consumer table + materialized view into a
MergeTree target**:

```
kafka_events (ENGINE = Kafka)  --MV-->  events_mt (ENGINE = MergeTree)
        consumer table                      durable, queryable storage
```

Deploy all three files together; query `events_mt`.

## Deploying

From a project that contains these files (under `datasources/` and `pipes/`):

```bash
tr deploy
```

`tr deploy` parses each `.datasource`, validates it, and issues the matching
`CREATE TABLE`:

- **Kafka** → `ENGINE = Kafka() SETTINGS kafka_broker_list=…, kafka_topic_list=…, …`
  Every `ENGINE_KAFKA_*` option maps to the `kafka_*` setting of the same name
  (minus the `ENGINE_` prefix, lower-cased).
- **S3** → `ENGINE = S3('<path>', ['<key>', '<secret>',] '<format>'[, '<compression>'])`
  from `ENGINE_S3_PATH` / `ENGINE_S3_FORMAT` (+ optional credentials/compression).
- **PostgreSQL** → `ENGINE = PostgreSQL('host:port', 'database', 'table', 'user', 'password'[, 'schema'])`
  from `ENGINE_POSTGRES_*` (the `ENGINE_PG_*` spelling is also accepted).

For Kafka, deploy also creates the materialized view (`kafka_to_events.pipe`)
into the MergeTree target.

## Secrets

Never commit credentials. Prefer node IAM roles (S3), read-only DB roles
(PostgreSQL), or [ClickHouse named collections](https://clickhouse.com/docs/en/operations/named-collections)
configured server-side. The static-key fields exist for local dev only.

## Advanced sources

Connector capability tracks ClickHouse's engine support, not a TinyRaven roadmap.
Anything ClickHouse can declare as an engine or table function works — `url()`,
`file()`, `MaterializedPostgreSQL` (Postgres CDC), `MySQL`, etc. Use the same
`ENGINE "<name>"` + `ENGINE_*` shape; the MergeTree-only keys
(`ENGINE_SORTING_KEY`/`ENGINE_PARTITION_KEY`/`ENGINE_TTL`) simply don't apply.
