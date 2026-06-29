# Monetization: sustainability-only, free core forever

TinyRaven is **not** monetized as a business. The objective is **sustainability**, not revenue. The software stays 100% free and feature-complete forever — every feature ships in the Apache-2.0 self-host build, with no paid tier, no gated features, and no "enterprise edition." Money, if any ever arrives, comes only from **optional donations / GitHub Sponsors** to offset the maintainer's costs (demo hosting, domain, time). This follows directly from mission **M0** — "create software that is free and genuinely useful to other people" — and the ScyllaDB→Cassandra positioning, whose entire credibility rests on the OSS being the real, complete thing.

A future **optional commercial layer** (managed hosting and/or paid support) is left explicitly *open* but is **not** being designed or built. It may only ever sit *beside* the free product — never by removing or gating a feature from self-host. That decision is deferred until real demand exists (YAGNI).

## Status

accepted

## Considered Options

- **No monetization at all** — pure gift, funded by the maintainer's day job. Viable, but forecloses ever accepting sponsorship; rejected only because leaving the donation door open costs nothing.
- **Sustainability-only (donations/sponsors), free core forever** — chosen.
- **Optional commercial layer beside an untouched free core** — kept as a *future option*, not built now.
- **Open-core (some features proprietary/paid)** — **rejected.** Directly violates M0 and the README's "no paywall" promise, and would gut the "the OSS is real" positioning the project is built on. The hard rule: no feature is ever removed from self-host to sell it.

## Consequences

- Apache-2.0 stays (see ADR on license choice). A permissive license means a competitor *could* fork and sell TinyRaven; that is an accepted consequence of M0, not a bug to close.
- There is no revenue model to protect, so no business pressure is allowed to justify gating a feature, adding telemetry-for-upsell, or crippling self-host. If those are ever proposed, this ADR is the thing they must supersede.
- Monetization specifics (donation channel, the free/paid boundary line for any future managed layer, trademark posture) live in `docs/MONETIZATION.md`.
