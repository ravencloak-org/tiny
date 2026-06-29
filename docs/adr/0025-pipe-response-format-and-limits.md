# Pipe responses: FORMAT JSON passthrough, no injected LIMIT, ceiling via CH profile

`GET /v0/pipes/{name}.{ext}` maps each extension to a ClickHouse `FORMAT` and passes the output through with minimal reshaping:

- **`.json` → ClickHouse `FORMAT JSON`.** CH's `JSON` format emits *exactly* Tinybird's pipe envelope — `{meta, data, rows, rows_before_limit_at_least, statistics}` — so we forward it almost verbatim. This is **less code** than streaming `JSONEachRow` and hand-assembling the envelope, and it is the correct parity shape. `/v0/sql` uses the same `FORMAT JSON` envelope.
- **`.ndjson` → `JSONEachRow`**, **`.csv` → `CSVWithNames`**, etc.

**No injected LIMIT (parity).** We do not parse pipe SQL to add a default `LIMIT`. Tinybird has no platform-wide default limit; the author controls rows with their own params (`LIMIT {{Int32(page_size, 100)}} OFFSET {{Int32(page, 0) * Int32(page_size, 100)}}`), and pagination is therefore zero TinyRaven code. Adding a `LIMIT` in the SQL is what surfaces `rows_before_limit_at_least`, which CH includes in `FORMAT JSON` automatically.

**Safety ceiling via the ClickHouse query profile, not SQL.** A pipe with no `LIMIT` (`SELECT * FROM huge`) would otherwise stream an entire table. We cap it the same way `/v0/sql` is capped (ADR 0011): the query runs under a profile with `max_result_rows`, `max_result_bytes`, and `max_execution_time` set (configurable, generous defaults). ClickHouse enforces the ceiling; TinyRaven parses nothing.

## Considered Options

- **Stream `JSONEachRow` and build the `{meta,data,rows,statistics}` envelope ourselves** — rejected. More code, and we'd have to synthesize `rows_before_limit_at_least` and `statistics` that CH already produces in `FORMAT JSON`.
- **Parse SQL to inject a default `LIMIT`** — rejected. Breaks the no-SQL-parsing stance (ADR 0011) and diverges from Tinybird (no default limit). A CH profile ceiling is the lazy, parser-free guard.

## Consequences

- `chi` is not involved in any of this — it routes `/v0/pipes/{name}.{ext}`; the format choice is which `FORMAT` we ask CH for, and the ceiling is CH profile settings on the query user.
- `FORMAT JSON` buffers the result set server-side in ClickHouse to compute `rows`/`statistics`, so it is not row-streamed. The profile ceiling (`max_result_rows`/`max_result_bytes`) is what bounds memory; truly large exports should use `.ndjson` (`JSONEachRow`, streamable) instead.
- Supersedes the incidental "stream `FORMAT JSONEachRow`" phrasing in ADR 0013 for `.json` pipe and `/v0/sql` responses.

## References

- [Tinybird Pipes API endpoints](https://www.tinybird.co/docs/api-reference/pipe-api/api-endpoints) · [query parameters / pagination](https://www.tinybird.co/docs/query/query-parameters.html)
- Builds on ADR 0011 (`/v0/sql` readonly profile) and ADR 0013 (native-insert / HTTP-query split).
