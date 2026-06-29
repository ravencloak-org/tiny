# Branches are schema-only with explicit lifecycle (no drop-on-merge)

Creating a branch (`tr_<branch>`) creates the tables with **no data** — schema only. Branch databases are dropped **explicitly** via `tr branch rm <name>`, never automatically on an inferred git merge.

## Data

- New branch = schema-only, empty. Matches Tinybird (branches start without prod data).
- Prod data is **never auto-copied** into a branch — main may hold billions of rows / TB; per-branch copies are infeasible.
- A `--with-sample N` option (bounded fixture load) is a later addition, not MVP.

## Lifecycle

- `tr branch rm <name>` drops `tr_<name>`. Explicit, intentional.
- The earlier "drop on merge" idea is **rejected**: TinyRaven cannot reliably detect a merge (GitHub vs local, squash, rebase). Auto-dropping a database on an inferred merge risks destroying data on a false positive.
- Optional `tr branch prune` may list branch DBs with no matching git branch and drop them **after explicit confirmation** — never silent.

## Consequences

- Can't test a pipe against real prod data on a branch without loading fixtures. Accepted — the only scalable option.
- Branch creation is cheap and instant (DDL only), which keeps the per-branch workflow fast.
