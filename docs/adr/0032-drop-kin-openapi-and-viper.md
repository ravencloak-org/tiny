# Drop kin-openapi and viper — emit-only spec from structs, hand-rolled config precedence

A ponytail audit cut two locked dependencies that did more than TinyRaven needs. Both were previously marked "Final"; this ADR records the reversal and why.

**`getkin/kin-openapi` → removed.** It exists to parse, validate, and resolve `$ref`s in *arbitrary* OpenAPI documents. TinyRaven only ever **emits** one spec, from a known pipe-registry shape (ADR 0017) — it never reads or validates third-party OpenAPI. The spec is a bounded JSON shape (a static `/v0` base fragment + one path per deployed endpoint-pipe, with typed params and a best-effort response schema). We define the ~dozen structs we actually emit and marshal them with stdlib `encoding/json`. No parsing, no validation, no ref resolution — none of what the library is for. We control our own output, so validating it against the OpenAPI schema is unnecessary.

**`viper` → removed (`cobra` stays).** viper resolves config precedence across flags, env, and files, dragging a large transitive tree. TinyRaven's config is three sources and a handful of keys (host, token, a few flags). Precedence — flag > `TINYBIRD_*` env > project `.tr/config.yml` > home `~/.tinyraven/config.yml` > defaults — is ~20 lines over `cobra`'s own flags + `os.Getenv` + one `yaml.Unmarshal`. The YAML files still need a parser, so `gopkg.in/yaml.v3` is added as a direct dependency — small, single-purpose, and far lighter than viper's tree. `cobra` is kept (it earns its place on help, completion, and nested commands like `tr branch rm`).

## Considered Options

- **Keep kin-openapi** — rejected: a parse/validate library used purely to marshal a fixed struct set is dead weight; `encoding/json` does the emit.
- **Keep viper** — rejected: 3-source precedence over a few keys is ~20 lines; viper's transitive tree isn't worth it.
- **Drop cobra too (stdlib `flag` + switch)** — rejected: cobra pays for itself on nested subcommands, help, and shell completion. The audit flagged it only as borderline; kept.
- **Hand-roll a YAML parser instead of adding `yaml.v3`** — rejected: YAML is not something to hand-parse; `yaml.v3` is the minimal correct choice.

## Consequences

- Net dependency change: **−2** (`kin-openapi`, `viper`), **+1** (`yaml.v3`) → one fewer dep and a much smaller tree.
- We now own a small set of OpenAPI-3 structs for the subset we emit; if we ever need to emit a far richer spec, revisit (unlikely — the surface is fixed by Tinybird parity).
- Config precedence is now explicit code, not viper magic — easier to read, one place to change, and it's the kind of glue ADR 0001-era "reuse before reinvent" still calls cheap.

## References

- Amends ADR 0017 (OpenAPI spec source) — implementation library changes; the runtime-from-registry decision stands.
- Audit: ponytail repo-wide pass, 2026-06-29.
