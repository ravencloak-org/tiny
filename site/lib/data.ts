// Single source of truth for all pricing and benchmark numbers shown on the
// marketing site. The orchestrator overwrites `benchmark` with real load-test
// figures — keep this module the only place these numbers live.

export const benchmark = {
  // REAL: measured locally on Apple Silicon (M-series) — 50 concurrent clients,
  // batched NDJSON (batch 1000), 15s window, 22.7M events ingested + persisted
  // to ClickHouse. TinyRaven's own numbers; no vendor head-to-head.
  tinyraven: {
    throughput: 1514615,
    p50ms: 19.8,
    p95ms: 79,
    p99ms: 178,
    note: "1 node · local M-series · 50 clients · batch 1000 · 15s · 22.7M events → ClickHouse (measured)",
  },
  source:
    "TinyRaven local load test — single commodity node, not vendor-tuned. No Tinybird head-to-head: we don't publish numbers we didn't measure.",
} as const;

export const pricing = {
  tinyraven: {
    plan: "Self-hosted (Apache 2.0)",
    monthly: "infra only (~$60 t3.large)",
    events: "unbounded (your CH)",
    retention: "your disk",
  },
  tinybird: {
    plan: "Tinybird managed",
    monthly: "usage-based ($$$)",
    events: "metered",
    retention: "plan-limited",
  },
} as const;

// Convenience rows for the comparison table, derived from `pricing` so the
// table never hardcodes its own copies of these values.
export const pricingRows = [
  { label: "Plan", tinyraven: pricing.tinyraven.plan, tinybird: pricing.tinybird.plan },
  { label: "Monthly cost", tinyraven: pricing.tinyraven.monthly, tinybird: pricing.tinybird.monthly },
  { label: "Events", tinyraven: pricing.tinyraven.events, tinybird: pricing.tinybird.events },
  { label: "Retention", tinyraven: pricing.tinyraven.retention, tinybird: pricing.tinybird.retention },
] as const;

export const links = {
  github: "https://github.com/ravencloak-org/tiny",
  docs: "https://github.com/ravencloak-org/tiny#readme",
} as const;
