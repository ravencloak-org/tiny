# Tinybird `/v0` API parity gaps

Audit of TinyRaven's implemented `/v0` surface (`internal/api/server.go` + handlers)
against Tinybird's documented `/v0` API. Ranked biggest user-facing gap first.

## What we already implement

| Endpoint | Method | Status | Notes |
|---|---|---|---|
| `/v0/events` | POST | **done** | Streaming ingest (JSON / NDJSON, gzip). `?name=<ds>`, `APPEND` scope. |
| `/v0/sql` | GET/POST | **done** | Read-only query proxy. `readonly=2` + caps (ADR 0011). `ADMIN` scope. |
| `/v0/pipes/{name}.json` | GET | **done** | Published pipe endpoint → `{meta,data,rows,statistics}`. `READ:<pipe>` scope. |
| `/v0/openapi.json` | GET | done (tr-native) | Runtime spec from registry (ADR 0017). Not a Tinybird endpoint. |
| `/v0/metrics` | GET | done (tr-native) | Prometheus scrape. Not a Tinybird endpoint. |
| `/health`, `/health/ready` | GET | done | Liveness/readiness (ADR 0024). |

The **data plane is complete**: a Tinybird client can ingest (`/v0/events`),
query published endpoints (`/v0/pipes/{name}.json`), and run ad-hoc SQL
(`/v0/sql`) by changing only `TINYBIRD_HOST`. The remaining gaps are the
**management / introspection** surface and **alternate response formats**.

## Ranked gaps

| # | Endpoint | Method | Tinybird behavior | Our status | Effort |
|---|---|---|---|---|---|
| 1 | `/v0/datasources` | GET | List datasources with schema (`{"datasources":[{name,columns,engine,...}]}`). Used by `tb datasource ls`, SDK/client introspection, "does my schema exist" checks. | **missing** | S |
| 2 | `/v0/pipes` | GET | List pipes/endpoints (`{"pipes":[{name,type,nodes,endpoint,...}]}`). `tb pipe ls`, UI/CLI discovery of queryable endpoints. | **missing** | S |
| 3 | `/v0/datasources/{name}` | GET | Single datasource metadata (schema, engine, stats). | **missing** | S |
| 4 | `/v0/pipes/{name}` | GET | Single pipe definition (nodes, SQL, endpoint node) — note: no `.json`. | **missing** | S–M |
| 5 | `/v0/pipes/{name}.{csv,ndjson,parquet,prometheus}` | GET | Same query, alternate output formats. Apps request `.csv`/`.ndjson` directly. Today only `.json` is served. | **missing** | M (executor must request alt CH `FORMAT` + content-type) |
| 6 | `/v0/datasources` (import) | POST | Batch import / `mode=append`/`replace`, CSV/NDJSON/Parquet file upload, returns an async job. `tb datasource append`, ETL clients. | **missing** | L (multipart, CSV parse, jobs) |
| 7 | `/v0/tokens` (+ `/{id}`) | GET/POST/PUT/DELETE | Token management API. We manage tokens via the `tr token` CLI only (`auth.Store.List` already exists). | **missing** (HTTP) | M |
| 8 | `/v0/jobs`, `/v0/jobs/{id}` | GET | Async job listing/status (pairs with #6). | **missing** | M |
| 9 | Copy pipes / scheduled (sink) pipes | POST | `TYPE copy`, schedules, on-demand `/v0/pipes/{name}/copy`. | **missing** | L |
| 10 | `/v0/datasources/{name}` ops | DELETE / POST | Delete, truncate, ALTER (add column), rename. | **missing** | M |

## Design context (why the ranking looks like this)

TinyRaven treats **git `.datasource`/`.pipe` files as the source of truth**, with
`tr deploy` populating the Redis/in-memory registry (ADR 0001, 0020, 0029). That
makes the **write/CRUD** management endpoints (#6, #9, #10, parts of #7) a
deliberate non-goal for now — you change definitions in git and `tr deploy`, not
over HTTP. The highest-value parity wins are therefore the **read/introspection**
endpoints (#1–#4) and **alternate output formats** (#5): they serve real client
and SDK traffic and reuse the existing registries/executor without contradicting
the architecture.

## #1 implemented in this change

`GET /v0/datasources` — lists registered datasources with their columns and
engine, wrapped in Tinybird's `{"datasources":[...]}` envelope. Pure read over
the existing `model.DatasourceRegistry`. `ADMIN`-gated (enumerating every
datasource schema is privileged, mirroring `/v0/sql`).

**Known parity delta:** Tinybird returns a token-scope-*filtered* list (a
non-admin token sees its accessible subset, not 403) and includes
`id`/`created_at`/statistics fields we don't synthesize. We expose `name`,
`columns` (`name`/`type`/`nullable`) and `engine` — the fields a client needs to
discover schemas — and gate the whole endpoint to `ADMIN` rather than
fabricating ids or building per-datasource read-scope filtering. Scope-filtered
listing is the follow-up (depends on a `READ`-datasource scope primitive we
don't have yet; today scopes are `APPEND:<ds>` and `READ:<pipe>`).
