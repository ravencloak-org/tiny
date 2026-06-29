# Pipe templating: parameterized ClickHouse queries, not string interpolation

Pipe value params (`{{Type(name, default)}}`) compile to ClickHouse server-side parameterized queries — `{name:Type}` placeholders plus a params map passed as `param_<name>=...` over the ClickHouse HTTP interface. Values are never string-interpolated into SQL. Go's `text/template` is explicitly rejected for value substitution.

Control flow (`{% if %}`, `{% for %}`, `defined()`, function set) is handled by a hand-written Jinja-flavored evaluator on top, since it shapes SQL *structure* and cannot route through value params. Structural inputs (identifiers) are allowlisted, never raw-interpolated.

## Scope

- **MVP:** the common, most-used subset — `{% if/elif/else %}`, `{% for %}`, `defined()`, the type functions (`String/Int*/Float*/DateTime/UUID/Boolean`), and `column()`. Enough to run the pipes real users write.
- **Later (Phase 2+):** full Tinybird template function catalog (the long tail: `Array`, `enumerate`, `sql_and`, etc.). Gaps tracked as issues.

## Why

- **Injection is structural, not a matter of careful escaping.** Routing values through ClickHouse parameters makes value-param SQL injection impossible by construction. `text/template` interpolates strings — the opposite property.
- **Parity is about behavior.** Parameterized queries reproduce Tinybird's value-substitution behavior while being safe; control flow gets a real evaluator so parity extends to dynamic SQL.

## Consequences

- Two layers in the pipe engine: a template evaluator (control flow → final SQL text) and a value-param compiler (→ CH `{name:Type}` + params). They compose: values inside control-flow blocks still bind as CH params.
- MVP will not run pipes that depend on deferred long-tail functions — a time-boxed parity gap, tracked, not abandoned.
