# Datasource/pipe validation: Go does structural+referential, ClickHouse owns semantics, deploy validates-all-then-applies

Validation of `.datasource` / `.pipe` files splits along a deliberate line, extending ADR 0008 ("TinyRaven parses, it does not reinterpret"):

**Go (parse/deploy time) — cheap, structural + referential only:**
- The file parses and required blocks are present (`SCHEMA` for a datasource; `NODE`/`ENDPOINT`/`MATERIALIZATION` for a pipe).
- **Referential check:** columns named in `ENGINE_SORTING_KEY`, `ENGINE_PARTITION_KEY`, and `ENGINE_TTL` exist in the `SCHEMA` column list. This catches the most common real error — a typo'd key column — before touching ClickHouse, with a clear file:line message. It requires parsing the `SCHEMA` column **names** (not modeling their types), a parse step the file format needs anyway.

**ClickHouse — the authority on everything semantic:**
- Column type validity (`Strng`, `DateTime64(99)`), engine-parameter correctness, function names in TTL expressions. We do **not** reimplement ClickHouse's type system. CH rejects at `CREATE TABLE`, surfaced through the error envelope (ADR 0012, `X-DB-Exception-Code`).

**`tr deploy` is validate-all-then-apply:**
- Parse + run the Go checks on **every** changed `.datasource`/`.pipe` first. Only if all pass does it mutate ClickHouse, under the per-branch deploy lock (ADR 0016). A typo in one file never leaves a half-deployed `tr_{branch}`. The `CREATE TABLE` itself is the final semantic gate; a CH rejection fails the deploy with CH's own error.

## Considered Options

- **Validate types/engine params in Go too** — rejected: reimplements ClickHouse's type system, guarantees drift, and contradicts ADR 0008. CH is the cheaper, always-correct authority.
- **Zero Go semantic checks — let CH catch missing columns too** — rejected: a typo'd sorting-key column would only surface as a CH `CREATE TABLE` error mid-deploy, worse DX than a parse-time file:line message, for a check that costs almost nothing.
- **Apply files one-by-one as parsed** — rejected: a later file failing leaves earlier ones applied (partial deploy). Validate-all-first avoids it.

## Consequences

- The Go datasource parser must extract the `SCHEMA` column-name set; keep it to names + raw type strings (forwarded verbatim), never a type model.
- "Validate all then apply" bounds atomicity at the validation layer, not the CH layer: if file 3 of 5 passes Go checks but CH rejects its `CREATE TABLE`, files 1–2 may already be applied. Full transactional rollback across CH DDL is out of scope; the deploy reports which step failed. (ponytail: per-file CH rollback only if partial-deploy pain shows up in practice.)
- Parity with Tinybird's `tb check` (datafiles validated before push).
