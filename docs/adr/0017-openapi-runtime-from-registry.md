# OpenAPI spec generated at runtime from the pipe registry

We serve `/v0/openapi.json` built **at runtime from the pipe registry**, not from a checked-in spec or handler annotations. The fixed `/v0` surface (events, sql, datasources, auth, metrics) is a small static base fragment (frozen for Tinybird parity); the dynamic per-pipe endpoints are emitted by enumerating deployed endpoint-pipes and mapping each pipe's `{{Type(name)}}` value-params to typed query parameters, with the `JSONEachRow` columns as the response shape. The spec is assembled from our own minimal OpenAPI-3 structs marshaled with stdlib `encoding/json`, then merged with the base — **not** `kin-openapi`, since we only ever emit a fixed-shape spec, never parse or validate arbitrary OpenAPI (dep cut, ADR 0032).

## Considered Options

- **`swaggo/swag`** (annotation → codegen) — rejected. It generates from handler annotations and cannot see per-deployment dynamic pipe paths; parity *requires* the spec reflect this box's actual pipes (Tinybird behaves the same).
- **Hand-maintained `openapi.yaml`** — rejected for the dynamic half (drifts per deployment). Kept only as the tiny static base for the frozen `/v0` surface.
- **Runtime build from registry, marshaling our own structs with `encoding/json`** — chosen. The param metadata is already parsed for query execution (issue #7), so the generator just reads the registry — near-zero new parsing. (Originally specced with `kin-openapi`; cut as emit-only dead weight — ADR 0032.)

## Consequences

- Spec always matches the deployed pipes — no drift, no separate maintenance step.
- **Ceiling:** response schemas are best-effort (CH column type → JSON type); exotic CH types may render generic. Acceptable for a discovery spec.
- Swagger UI is deferred (separate issue); only the JSON document ships first.

## Amendment — docs UI: `/tr/v1/docs`, embedded single-file renderer, off by default

Resolving the deferral. A human-browsable UI is a TinyRaven-native convenience (Tinybird serves no per-workspace API UI), so it lands under the native namespace `/tr/v1/docs` (ADR 0029), not `/v0`. It renders the existing `/v0/openapi.json` — no second spec.

- **Renderer:** a single-file bundle (Scalar or Redoc standalone), **`go:embed`-ed into the binary** — not full Swagger UI (a multi-file ~3 MB dist) and not CDN-loaded. Self-hosted deployments must work air-gapped; embedding keeps it self-contained with no external fetch, at ~hundreds of KB of binary growth.
- **Off by default**, enabled with `docs_enabled: true` — mirrors the CORS secure-default (ADR 0026). The spec exposes pipe names and parameters, so the UI is never served unless the operator opts in.
- Not ADR-significant on its own (swapping a docs renderer later is trivial); recorded here so the deferral has a resolution.
