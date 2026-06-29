# tr deploy auto-applies safe migrations and refuses breaking ones

`tr deploy` classifies the schema diff between the project files and live ClickHouse, then:

- **Safe changes auto-apply.** Adding a nullable or defaulted column.
- **Breaking changes are refused by default.** Drop column, rename column, type change, sorting-key change, partition-key change, engine change. `tr deploy` prints the diff and the reason, applies nothing, and exits non-zero.

No destructive or table-rewriting operation runs from a file diff without explicit opt-in. `tr deploy --dry-run` prints the classified diff and applies nothing.

## Phasing

- **MVP (Phase 2):** auto-apply safe; refuse breaking with a clear diff. That is the whole deploy migration surface.
- **Phase 3:** the guarded breaking path — shadow table → MV backfill → atomic `EXCHANGE TABLES` — behind an explicit `tr deploy --allow-breaking` (or a confirmed migration plan).

## Why

- A one-line edit to a `.datasource` file must never silently trigger an expensive, irreversible table rewrite. Refuse-by-default makes destructive intent explicit.
- ClickHouse DDL is not transactional; an auto-run breaking migration that fails midway leaves the table in an unknown state. Gating it behind a flag + the shadow-swap machinery (Phase 3) is the only safe way.

## Consequences

- At MVP, a breaking change cannot be applied by the tool — the user edits ClickHouse manually or waits for Phase 3. Accepted: safer than auto-rewriting from a diff.
- The diff classifier is the load-bearing component; misclassifying a breaking change as safe is a data-loss bug. It needs direct tests.
