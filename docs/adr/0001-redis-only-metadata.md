# Redis is the only metadata store; no PostgreSQL

TinyRaven stores all metadata — workspace state, pipe/datasource definitions, tokens, and deploy state — in **Redis** (AOF-persisted, mounted volume). ClickHouse holds the actual event data. There is no PostgreSQL, SQLite, or pluggable-backend matrix.

## Why

- **Tinybird does exactly this.** Tinybird uses Redis for workspace/pipe/token metadata and ClickHouse for data — no relational metadata DB. Matching it serves the parity goal and is a proven design.
- **Zero new dependency.** Redis is already in the stack (token cache, rate limiting). Postgres would add a second stateful service to every deploy path (Heroku add-on, AWS RDS, Compose/Helm container) — directly undercutting the "minimal infra / pay only infrastructure" positioning.
- **Git is the source of truth for definitions.** `.datasource` and `.pipe` files live in the user's repo. The metadata store is a *registry of what has been deployed*, rebuildable by re-running `tr deploy`. So definitions don't need transactional durability — only tokens and deploy state do, which Redis persistence covers.

## Consequences

- Redis runs as a **system of record**, not a throwaway cache: AOF persistence + a mounted volume are mandatory in every deploy template.
- No cross-store SQL transaction during `tr deploy`. Partial-deploy failures are handled by idempotent re-apply (re-run `tr deploy` from git), not rollback. ClickHouse DDL isn't transactional anyway, so this changes nothing in practice.

## Considered and rejected

- **PostgreSQL (prod) + SQLite (local) split** — two backends to build/test/document for an MVP; and the "local needs no server" premise fails because `tr local start` already runs Redis + ClickHouse in Compose.
- **ParadeDB** — a Postgres BM25/search extension; no metadata workload here needs full-text search.
