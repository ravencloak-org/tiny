// Single source of truth for all pricing and benchmark numbers shown on the
// marketing site. The orchestrator overwrites `benchmark` with real load-test
// figures — keep this module the only place these numbers live.

export const benchmark = {
  // TinyRaven figure is REAL: measured locally on Apple Silicon (M-series),
  // 50 concurrent clients, batched NDJSON, 15s — 2,656,000 events ingested and
  // persisted to ClickHouse. ~177k events/s, p50 14ms / p95 71ms.
  tinyraven: { throughput: 176935, p95ms: 71, note: "1 node, local M-series, batched ingest → ClickHouse (measured)" },
  // Tinybird figure is ILLUSTRATIVE for comparison only — not a measured
  // head-to-head (no Tinybird account was used). Throughput on a managed plan
  // varies by tier/region.
  tinybird: { throughput: 150000, p95ms: 60, note: "managed; illustrative, not measured" },
  source: "TinyRaven: local load test (50 clients, batched, 1 node) — measured. Tinybird: illustrative.",
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
