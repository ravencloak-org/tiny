# Pipe templating: parameterized ClickHouse queries, not string interpolation

Pipe value params (`{{Type(name, default)}}`) compile to ClickHouse server-side parameterized queries — `{name:Type}` placeholders plus a params map passed as `param_<name>=...` over the ClickHouse HTTP interface. Values are never string-interpolated into SQL. Go's `text/template` is explicitly rejected for value substitution.

Control flow (`{% if %}`, `{% for %}`, `defined()`, function set) shapes SQL *structure* and cannot route through value params. When built, it is a thin block tokenizer that delegates expression evaluation inside `{% %}` to **`github.com/expr-lang/expr`** (a mature, safe, sandboxed evaluator) — we do **not** hand-write the expression parser/evaluator, which is the hard, bug-prone part. Structural inputs (identifiers) are allowlisted, never raw-interpolated.

## Scope

- **MVP (Phase 1):** **value params only** — `{{Type(name, default)}}` → CH `{name:Type}` + params map. Zero template parser; pure substitution. Most real-world pipes need nothing more. This deletes the single hardest parser from MVP scope to accelerate the release.
- **Phase 2:** control flow (`{% if/elif/else %}`, `{% for %}`, `defined()`, `column()`), conditions evaluated via `expr-lang/expr`.
- **Phase 2+ (long tail):** full Tinybird template function catalog (`Array`, `enumerate`, `sql_and`, etc.). Gaps tracked as issues.

## Why

- **Injection is structural, not a matter of careful escaping.** Routing values through ClickHouse parameters makes value-param SQL injection impossible by construction. `text/template` interpolates strings — the opposite property.
- **Parity is about behavior.** Parameterized queries reproduce Tinybird's value-substitution behavior while being safe; control flow gets a real evaluator so parity extends to dynamic SQL.

## Consequences

- Two layers in the pipe engine: a template evaluator (control flow → final SQL text) and a value-param compiler (→ CH `{name:Type}` + params). They compose: values inside control-flow blocks still bind as CH params.
- MVP will not run pipes that depend on deferred long-tail functions — a time-boxed parity gap, tracked, not abandoned.
