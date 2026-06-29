# TinyRaven

Self-hosted, drop-in alternative to Tinybird: an HTTP analytics layer over OSS ClickHouse with Tinybird-identical APIs, SQL pipes, and a `tr` CLI.

## Language

**Datasource**:
A declared ClickHouse table — its schema and engine config — defined in a `.datasource` file. The destination for ingested events.
_Avoid_: Table (the underlying ClickHouse object), Stream.

**Pipe**:
A published, parameterized SQL query defined in a `.pipe` file, exposed as a REST endpoint at `/v0/pipes/{name}.json`.
_Avoid_: Query, View, Endpoint (an endpoint is what a pipe is *published as*, not the pipe itself).

**Gatherer**:
The in-process batching component that buffers ingested events and flushes them to ClickHouse on whichever comes first: a size threshold or a time interval.
_Avoid_: Buffer, Batcher, Queue.

**Metadata Registry**:
The Redis-backed record of what has been deployed (datasource + pipe definitions) plus runtime state (tokens, deploy state). It is *not* the source of truth for definitions — git is. The registry is rebuildable from git via `tr deploy`.
_Avoid_: Metadata DB, Catalog, Schema store.

**Workspace**:
The entire TinyRaven deployment — one project. TinyRaven is single-tenant: one workspace per deployment. Not a synonym for a branch or a database.
_Avoid_: Tenant, Project, Organization.

**Branch**:
An isolated copy of the data, mapped to one ClickHouse database named `tr_{branch}` (branch name sanitized to a valid identifier). Mirrors the current git branch.
_Avoid_: Workspace (a branch lives inside the workspace), Environment.

**Template**:
The Tinybird-compatible templating language inside a `.pipe`. Value params (`{{Type(name, default)}}`) compile to ClickHouse parameterized queries (`{name:Type}`); control flow (`{% if %}`, `{% for %}`) is evaluated to produce the final SQL. Never string-interpolated.
_Avoid_: Macro, Interpolation.

**Token**:
An opaque random bearer string whose scopes are stored in Redis. Authenticates API requests. Static/admin tokens never expire; resource-scoped tokens are declared in `.pipe`/`.datasource` files and created on `tr deploy`.
_Avoid_: API key, JWT (TinyRaven tokens are opaque, not signed claims), Secret.

**Materialized pipe**:
A `.pipe` with `TYPE materialization` that continuously writes into a target table. Maps to a ClickHouse incremental MV by default (backfilled on deploy), or a refreshable MV when the pipe declares `REFRESH EVERY`.
_Avoid_: Materialized view (the ClickHouse object), Rollup.

**Source of truth**:
The user's git repo. `.datasource` and `.pipe` files are authoritative; everything in the Metadata Registry derives from them (tokens excepted).
