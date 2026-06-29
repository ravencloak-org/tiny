# Monetization & Sustainability

> Canonical stance for how (and whether) TinyRaven makes money. Decisions here are anchored by [ADR 0021](adr/0021-monetization-sustainability-only.md) (sustainability-only) and [ADR 0022](adr/0022-license-apache-not-copyleft.md) (Apache 2.0).

## Frame: sustainability, not revenue

TinyRaven is a **gift**, not a business. The goal is to be free and genuinely useful to everyone (mission **M0**). There is no revenue target and no investor. "Monetization" here means **covering the maintainer's costs**, nothing more.

The software is **100% free and feature-complete, forever.** Every feature ships in the Apache-2.0 self-host build. No paid tier, no gated features, no "enterprise edition," no license keys.

A future **optional managed/commercial layer** is left *open* but is **not** designed or built. It may only ever sit *beside* the free product (see the invariant below). The decision to build it is deferred until real demand exists.

## The capability invariant (drift-proof line)

> **If it is a capability in the `tr` binary, it is free. The only thing a paid layer may ever charge for is our time or our servers — operating it for you — never the capability itself.**

Everything `tr` + ClickHouse + Redis can do **on hardware you own** is free, complete, and forever. Money — if it ever arrives beyond donations — comes only from **us running it for you** (managed hosting) or **us helping you** (support / consulting).

### Forbidden, even if a managed layer launches

These are open-core by another name and are off the table while ADR 0021 stands:

- License-keyed feature flags in the self-host binary
- A separate "enterprise" / proprietary build
- Artificial limits in OSS (row caps, token caps, **node/instance caps**) that a paid tier lifts
- Telemetry that exists to drive upsell
- "Sponsor-first" or delayed-OSS features (everything lands in OSS at the same time)
- **Paywalling security** — auth, SSO, passkeys, RBAC are never gated (the "SSO tax")

### Operate, don't gate — worked examples

The same money source (our infra + labor), the invariant intact:

| Feature | Self-host (`tr`) | Future managed cloud may charge for |
|---|---|---|
| Passkey / SSO auth | Free, present | — (security is never gated, even managed) |
| Multi-ClickHouse / multi-node | Free, present | **Operating** the cluster for you (HA, scaling, ops) |
| OTel adapter | Free, present | **Running** the OTel pipeline as a hosted service |
| Cloudflare-style tunnel | Free, present | **Terminating / managing** the tunnel for you |

The test for any future paid thing: *Am I charging for a capability, or for running infra / spending my time?* Only the second is allowed.

## Donations

[GitHub Sponsors](https://github.com/sponsors/jobinlawrance) — configured via `.github/FUNDING.yml`. No begging, no nags; just a Sponsor button. Optional, offsets demo hosting / domain / time. (Inert until the `jobinlawrance` account is enrolled as sponsorable.)

## Trademark

The **code** is Apache 2.0 — fork it freely, including to compete. The **name "TinyRaven" and the raven logo** are *not* licensed and are claimed as an unregistered trademark (™).

- You may build on, redistribute, and fork the code.
- You may **not** name your fork "TinyRaven", use the logo, or imply official endorsement / that your build is the official one.
- No formal registration (®) at this stage — deferred until there's something worth registering (YAGNI). The unregistered claim is asserted here and is enough to ask a confusing fork to rename.

This is the one lever a permissive license leaves intact, and the only thing that would give a future managed offering a distinct identity.
