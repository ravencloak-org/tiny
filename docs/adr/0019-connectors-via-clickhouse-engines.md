# Connectors (Kafka, S3, Postgres) are ClickHouse-native engines, not built services

TinyRaven does not build connector runtimes (Kafka consumer pools, S3 pollers, schedulers, offset/DLQ management). Instead, a `.datasource` declares a ClickHouse native engine — `ENGINE = Kafka(...)` + a materialized view, `s3()`/`url()`/`file()` table functions, `ENGINE = PostgreSQL(...)` — and `tr deploy` creates those CH objects. **ClickHouse does the pulling.** The HTTP Events API (the Gatherer) remains the only first-class, TinyRaven-owned ingestion path; Kafka/S3/Postgres land in Phase 5 as engine passthrough + `.datasource` templates + docs.

## Considered Options

- **Build managed connectors** (mirror Tinybird: TinyRaven runs the Kafka consumers, S3 pollers, offset tracking, a connect UI) — rejected. Enormous surface, off-mission for a lean self-hosted tool, and duplicates capability ClickHouse already ships.
- **Lean on ClickHouse native engines** — chosen. Near-zero connector runtime; reuses the DDL-generation `tr deploy` already does for ordinary datasources.

## Consequences

- **Parity gap (state loudly in docs):** Tinybird connectors are *managed* — its infra runs consumers, offers a UI, handles offsets/dead-letter. TinyRaven's are *CH-native* — the user declares and operates the CH engine. Same data lands; the operational surface differs. A drop-in user migrating a Kafka pipeline gets "declare a CH Kafka engine," not "click Connect."
- Connector capability tracks ClickHouse's engine support, not a TinyRaven roadmap — new sources come from CH, not from us.
- A future contributor is explicitly steered *away* from writing a bespoke Kafka consumer; this ADR is the answer to "why isn't there one?"
