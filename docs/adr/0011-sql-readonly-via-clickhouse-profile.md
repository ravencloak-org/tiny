# /v0/sql read-only is enforced by ClickHouse, not by parsing SQL

`/v0/sql` and the pipe read path run under a dedicated ClickHouse user/role with a `readonly=2` profile plus resource caps (`max_execution_time`, `max_result_rows`, `max_memory_usage`, `max_rows_to_read`). TinyRaven does **not** parse SQL to block writes. Ingestion and `tr deploy` DDL use a separate read-write ClickHouse user.

## Why

- Hand-rolled SQL blocklists are a perennial bypass source: comments, multi-statements, `INSERT ... SELECT`, table functions (`url()`, `file()`), `system.*` reads, and settings injection all defeat naive parsers.
- ClickHouse's `readonly=2` profile refuses writes, DDL, and settings changes structurally — there is no parser to outsmart. This is unbypassable in a way an app-level guard never is.

## Two ClickHouse identities

- **Read-write** — Gatherer flush, `tr deploy` DDL, migrations.
- **Read-only (`readonly=2` + caps)** — `/v0/sql`, pipe queries.

Token scope gates *who reaches the endpoint*; the ClickHouse profile guarantees *what the endpoint can do*.

## Consequences

- Two CH users/profiles to provision: `tr local start` sets them up automatically; BYO deployments document the required users.
- Resource caps prevent a single `/v0/sql` query from exhausting the cluster — DoS mitigation comes free with the profile.
