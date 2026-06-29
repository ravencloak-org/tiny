# Auth: opaque random tokens backed by Redis, not JWTs

TinyRaven authenticates with **opaque random bearer tokens**. The token string carries no information; its scopes live in the Redis metadata store (the persistent, AOF-backed record). A short-TTL validation cache fronts the lookup so the hot path is not a Redis round-trip per request.

## Model

- **Format:** opaque random string. Scopes stored against it in Redis. No JWT, no signing keys in the MVP.
- **Bootstrap:** on first server init / `tr local start`, auto-generate an `ADMIN` token, print it once, and write it to `~/.tinyraven/config.yml`. Idempotent — never regenerate if one already exists.
- **Scopes (MVP):** `ADMIN`, `WORKSPACE:READ_ALL`, and per-pipe `READ`. Resource-scoped tokens are declared in `.pipe` / `.datasource` files and materialized on `tr deploy`.
- **Expiry:** static/admin tokens **never expire** — a TTL on an admin token would silently brick deploys. TTL applies only to the validation cache and to short-lived client tokens.

## Why

- **Instant revocation.** Deleting the Redis key kills the token immediately. JWTs can't be revoked without a denylist — which is a Redis lookup anyway, erasing the stateless advantage.
- **No key management.** No signing key to generate, rotate, or leak.
- **Redis is already the system of record.** Tokens live where the rest of the metadata lives.

## Consequences

- One Redis GET per request on cache miss. Mitigated by the validation cache; acceptable given the throughput target is on ingestion, not authed query QPS.
- Stateless JWT verification and Tinybird-style short-lived client tokens are deferred. Closing that parity gap (client-side JWTs) is a later issue, layered on top — opaque static tokens stay the backbone.
