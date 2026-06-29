# Browser access: CORS now (default-off), JWT browser tokens deferred

Browser clients calling `/v0/pipes` / `/v0/sql` directly need CORS. We add it via `github.com/go-chi/cors` (the standard chi CORS middleware — this *is* in chi's ecosystem, unlike request decompression or health, which are our own code). Allowed origins are **configurable and default to empty**, i.e. browser access is **off by default** — a secure default that an operator opts into per deployment (`cors_allowed_origins`).

For MVP, a browser authenticates with a **read-scoped opaque token** (`PIPE:READ`, stored and revocable in Redis — ADR 0005). The accepted trade-off: an opaque token is **long-lived and exposed** once it sits in browser code, until explicitly revoked. This matches Tinybird's pre-JWT behavior (static tokens were usable in-browser), so it is defensible parity, but it is not the hardened path.

**JWT browser tokens (HS256, short-lived, scoped) are deferred to Phase 2** (tracked as [issue #63](https://github.com/ravencloak-org/tiny/issues/63)). JWT is Tinybird's recommended browser pattern and the safe one — a leaked JWT expires on its own and can carry fixed params / per-tenant scope — but it is real work and sits *beside* the opaque-token model (ADR 0005), not replacing it. We do not build it for MVP (YAGNI until multi-tenant browser scoping is actually needed).

## Considered Options

- **JWT in MVP** — rejected for now: meaningful work, conflicts in shape with opaque tokens (ADR 0005), and not needed until someone has a multi-tenant browser use case.
- **CORS default-on (allow `*`)** — rejected: insecure default; browser data access should be an explicit opt-in.
- **No CORS, require a backend proxy** — rejected: breaks drop-in parity for the common "call the pipe from the frontend" pattern Tinybird supports.

## Consequences

- Two auth shapes will coexist once JWT lands: opaque tokens (backend/trusted) and JWT (browser/multi-tenant). The token-validation path must branch on token type — design ADR 0005's validation with that future fork in mind.
- Until JWT ships, the docs must state plainly that a browser-exposed read token is long-lived and should be narrowly scoped + rotated.

## References

- [Tinybird browser access / JWTs](https://www.tinybird.co/docs/api-reference/pipe-api/api-endpoints) — HS256, signed shared secret, frontend-minted.
- Builds on ADR 0005 (opaque tokens in Redis).
