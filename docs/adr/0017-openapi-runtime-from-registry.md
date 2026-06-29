# OpenAPI spec generated at runtime from the pipe registry

We serve `/v0/openapi.json` built **at runtime from the pipe registry**, not from a checked-in spec or handler annotations. The fixed `/v0` surface (events, sql, datasources, auth, metrics) is a small static base fragment (frozen for Tinybird parity); the dynamic per-pipe endpoints are emitted by enumerating deployed endpoint-pipes and mapping each pipe's `{{Type(name)}}` value-params to typed query parameters, with the `JSONEachRow` columns as the response shape. The spec is assembled with `github.com/getkin/kin-openapi` (typed OpenAPI-3 structs + marshal), then merged with the base.

## Considered Options

- **`swaggo/swag`** (annotation → codegen) — rejected. It generates from handler annotations and cannot see per-deployment dynamic pipe paths; parity *requires* the spec reflect this box's actual pipes (Tinybird behaves the same).
- **Hand-maintained `openapi.yaml`** — rejected for the dynamic half (drifts per deployment). Kept only as the tiny static base for the frozen `/v0` surface.
- **Runtime build from registry via `kin-openapi`** — chosen. The param metadata is already parsed for query execution (issue #7), so the generator just reads the registry — near-zero new parsing.

## Consequences

- Spec always matches the deployed pipes — no drift, no separate maintenance step.
- **Ceiling:** response schemas are best-effort (CH column type → JSON type); exotic CH types may render generic. Acceptable for a discovery spec.
- Swagger UI is deferred (separate issue); only the JSON document ships first.
