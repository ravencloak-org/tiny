# API versioning: `/v0` is a frozen Tinybird mirror; native features live under `/tr/v1`

TinyRaven runs two independent URL-version lines.

- **`/v0` is the Tinybird parity surface, frozen.** It only ever mirrors Tinybird's `/v0` — same paths, shapes, and semantics. We never add a TinyRaven-only route under `/v0`, and we never bump it for our own reasons. "Drop-in: change `TINYBIRD_HOST` and existing client code works" depends on `/v0` meaning exactly what Tinybird's `/v0` means, indefinitely.
- **TinyRaven-native endpoints live under `/tr/v1/...`.** Anything Tinybird has no equivalent for goes under the `/tr/` namespace (matches the binary name), versioned independently as `/tr/v1`, `/tr/v2`, … This is ours to evolve on our own cadence.
- **The parity line tracks Tinybird.** If Tinybird ever ships a `/v1`, we add a matching `/v1` mirror to preserve parity; the existing `/v0` stays frozen as the v0 mirror. Tinybird drives the parity version numbers; we drive only the `/tr/vN` line.

## Considered Options

- **Put native features under `/v1`** — rejected. Bumping to `/v1` reads as "v0 is the old/deprecated version," which is false — `/v0` is a permanent compatibility contract, not a deprecated release. A client treating the number as semver would misread `/v1` as superseding `/v0`.
- **Extend `/v0` with `/v0/<our-feature>`** — rejected. It pollutes the mirror and risks a hard collision if Tinybird later ships a route at the same path; the parity guarantee then breaks silently.
- **`/tr/v1` namespace, `/v0` frozen** — chosen.

## Consequences

- **Surprising-without-context (state in docs):** TinyRaven's own features are at `/tr/v1/...`, not `/v1/...`. A reader expecting "next version = `/v1`" needs to know `/v1` is reserved to mirror a hypothetical Tinybird `/v1`, and native work is deliberately quarantined under `/tr/`.
- Two version lines must be routed distinctly in `chi`; the split is also the natural place to gate "is this endpoint a parity promise or a TinyRaven extension."
- A TinyRaven feature that later becomes a Tinybird feature can be mirrored into the parity surface without moving its `/tr/` form immediately — the two can coexist during transition.
- Keeps the OpenAPI spec (ADR 0017) cleanly partitionable: `/v0` = parity contract, `/tr/v1` = native surface.
