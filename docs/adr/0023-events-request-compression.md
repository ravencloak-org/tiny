# /v0/events: gzip + zstd request bodies, cap on compressed bytes, streamed decompression ceiling

`POST /v0/events` accepts compressed request bodies via `Content-Encoding: gzip` or `Content-Encoding: zstd`, auto-decompressed before NDJSON parsing — matching Tinybird's Events API. `gzip` uses stdlib `compress/gzip`; `zstd` uses `github.com/klauspost/compress/zstd`, which arrives transitively through `clickhouse-go` (its native protocol already depends on it), so this adds no new **direct** dependency.

Two size limits, deliberately distinct:

1. **Wire cap (parity, the 413 line).** The configurable max body size (default 10 MB, see ADR 0018) is enforced on the **compressed bytes actually received** — so compression lets a client pack more raw NDJSON under the limit, exactly as Tinybird documents. Exceed it → `413`.
2. **Decompressed ceiling (security).** A small gzip/zstd body can expand to gigabytes (decompression bomb). The body is **stream-decompressed through a hard decompressed-byte ceiling** (configurable, default 256 MB) and aborted with `413` the moment it's exceeded. We never `io.ReadAll` an attacker-controlled decompressor into memory. This guard is stated explicitly because it is a security boundary, not an incidental limit.

## Considered Options

- **gzip only** — rejected; breaks drop-in parity for zstd clients, and zstd is already in the dependency tree.
- **Cap on decompressed size instead of wire bytes** — rejected; contradicts Tinybird's contract ("size limits refer to the request payload; with compression you fit more raw data") and would make a client's 413 depend on its compression ratio.
- **Decompress fully, then check size** — rejected; that *is* the decompression-bomb vulnerability. Must enforce the ceiling mid-stream.

## Consequences

- Two configurable knobs, not one: `max_body_bytes` (compressed/wire, default 10 MB) and `max_decompressed_bytes` (default 256 MB). Operators on big boxes can raise both.
- Uncompressed requests (no `Content-Encoding`) only hit the wire cap — the decompressed ceiling is a no-op for them.
- An unsupported `Content-Encoding` value is `415 Unsupported Media Type`, not a silent pass-through.
- chi does **not** decompress request bodies — its `middleware.Compress` is response-side only. Decompression is our own code in the events path: switch on `Content-Encoding`, wrap `r.Body` in `gzip.NewReader` / `zstd.NewReader`, then `io.LimitReader` to the decompressed ceiling. No router middleware involved.

## References

- [Tinybird Events API — compressed ingestion](https://www.tinybird.co/docs/api-reference/events-api)
- Builds on ADR 0018 (per-row validation + quarantine, body cap + 413).
