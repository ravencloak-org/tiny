# Error parity is structural: envelope + status codes + X-DB-Exception-Code, not message text

TinyRaven matches Tinybird's error *contract*, not its human-readable message strings:

- **Body:** JSON `{"error": "<message>"}` (Tinybird's shape; not RFC 7807 `problem+json`).
- **Status codes:** 400 caller-fixable, 401/403 auth, 404 not-found, 429 rate-limited, 500 server.
- **Header:** on query failure, `X-DB-Exception-Code` carries the underlying ClickHouse exception code (stringified number), passed through from ClickHouse.

## Why

- Integrations depend on status code, the JSON envelope, and the DB-error header — not on the prose of the message. A client that regex-matches literal error text is already broken.
- `X-DB-Exception-Code` is free: TinyRaven proxies ClickHouse, so CH supplies its error code; TinyRaven forwards it. No error-code reinvention.

## Consequences

- Message text will not be byte-identical to Tinybird. Accepted — out of scope, and unstable on Tinybird's side too.
- TinyRaven's own errors (auth, rate limit, not-found, validation) use the same envelope + status mapping so the surface is uniform.
