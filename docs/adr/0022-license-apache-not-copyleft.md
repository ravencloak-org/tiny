# License: Apache 2.0, not AGPL/SSPL/BSL

TinyRaven is licensed **Apache 2.0** — permissive — even though it is a drop-in clone of a commercial SaaS (Tinybird). We deliberately did **not** reach for the copyleft / source-available licenses that comparable projects (Redis, Elastic, MongoDB, HashiCorp, Sentry) adopted to stop competitors from monetizing their work.

The reason a reader would expect AGPL/SSPL here — protecting a revenue stream from a forking competitor or hyperscaler — does not apply: TinyRaven has no revenue model to protect ([ADR 0021](0021-monetization-sustainability-only.md)). Mission **M0** wants the software useful to *everyone*, including people who embed or relicense it and cannot touch AGPL's network-copyleft. ClickHouse, the engine underneath, is itself Apache 2.0, so staying permissive keeps the whole stack legally coherent. A competitor (even Tinybird) forking and selling TinyRaven is an **accepted, intended** consequence — not a loophole to close.

## Considered Options

- **Apache 2.0** — chosen. Max adoption, max reuse, coherent with the ClickHouse stack.
- **AGPL-3.0** — also free, and would force service forks to publish their changes (more M2-aligned). Rejected because network-copyleft measurably *reduces* adoption (enterprise legal avoids it), which works against M0.
- **SSPL / BSL** — source-available, not open source. Rejected outright: breaks the "free and genuinely useful" promise and the ScyllaDB→Cassandra positioning.

## Consequences

- Relicensing later is effectively impossible without every contributor's consent, so this is a one-way door — chosen knowingly.
- No CLA is required for the permissive grant; contributions come in under Apache 2.0 inbound=outbound.
