# ClickHouse access: native driver for inserts, HTTP interface for queries

TinyRaven talks to ClickHouse two ways, each chosen for its path:

- **Inserts (Gatherer flush):** `clickhouse-go` v2 native driver over the TCP protocol (9000). Batched columnar inserts — the path that reaches the 10k events/s goal.
- **Queries (`/v0/pipes`, `/v0/sql`):** ClickHouse HTTP interface (8123) via Go stdlib `net/http`. Stream `FORMAT JSONEachRow` straight to the client; clean fit for `X-DB-Exception-Code` passthrough (ADR 0012) and the `readonly=2` user (ADR 0011).

The inbound HTTP server is separate and already decided: stdlib `net/http` + `chi` router (no gin/echo/fiber).

## Why

- Native columnar batch insert is materially faster than HTTP insert — throughput is the ingestion goal.
- For queries, proxying ClickHouse's own JSON bytes avoids re-serializing rows through the driver, and the HTTP interface exposes the DB error code as a header for free.

## Consequences

- Two ClickHouse access paths + the `clickhouse-go` dependency. Accepted: each path uses the right tool; homogenizing onto one client costs either insert throughput or clean query/JSON/header passthrough.
- Connection config (host, users, pooling) must be wired for both the native port (9000) and HTTP port (8123).
