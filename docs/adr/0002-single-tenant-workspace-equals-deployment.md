# Single-tenant: one Workspace = one deployment; a Branch = one ClickHouse database

TinyRaven is single-tenant. A **Workspace** is the entire TinyRaven deployment (one project). A **Branch** is an isolated copy of the data, implemented as one ClickHouse database named `tr_{branch}` (branch name sanitized to a valid ClickHouse identifier, e.g. `feature-x` → `tr_feature_x`). There is no multi-workspace / multi-project tenancy in the box.

## Why

- **Self-hosted framing.** "You own the deployment" implies one project per deployment. Multi-workspace tenancy is a Tinybird *cloud* concern, not a self-hosted one.
- **Simpler RBAC.** Tokens scope to branches, not a workspace-vs-org hierarchy. Drops a whole layer of the Tinybird cloud token model.
- **Removes an overloaded term.** The docs used "workspace", "branch", and "database" interchangeably. Now: Workspace = deployment, Branch = database. "Workspace" no longer names a ClickHouse database — the `workspace_{branch}` naming is replaced by `tr_{branch}`.

## Consequences

- Token scopes like `WORKSPACE:READ_ALL` apply to the single deployment-wide workspace; resource scopes target branches/pipes within it.
- If multi-tenant (one TinyRaven serving independent teams) is ever wanted, it is a hard, deliberate change — single-tenant assumptions will be baked into token scoping and DB naming. Accepted: it contradicts the self-hosted positioning, so it is explicitly out of scope.
