import type { ComponentType, SVGProps } from "react";
import { cn } from "@/lib/utils";
import { links } from "@/lib/data";
import { Button } from "@/components/ui/button";
import { CodeBlock } from "./code-block";
import { SectionHeading } from "./section";
import {
  ArrowRightIcon,
  BoltIcon,
  DatabaseIcon,
  FileCodeIcon,
  GitBranchIcon,
  GitHubIcon,
  LayersIcon,
  PlugIcon,
  ServerIcon,
  TerminalIcon,
} from "./icons";

type Status = "live" | "clickhouse" | "partial";

type UseCase = {
  title: string;
  body: string;
  status: Status;
  note?: string;
  Icon: ComponentType<SVGProps<SVGSVGElement>>;
};

// Coverage reflects what the codebase actually does today — not aspiration.
// "live" = same .datasource/.pipe primitives, drop-in. "clickhouse" = works via
// a ClickHouse-native engine/function, not yet load-proven. "partial" = a real
// architectural boundary (stated, not buried).
const useCases: UseCase[] = [
  {
    title: "User-facing dashboards",
    body: "Ingest events, expose parameterized pipes as typed JSON endpoints. The flagship real-time dashboard pattern.",
    status: "live",
    note: "Demonstrated end-to-end",
    Icon: DatabaseIcon,
  },
  {
    title: "Gaming analytics",
    body: "Leaderboards, match-making signals, in-game ad targeting — all count/rank queries over the same event stream.",
    status: "live",
    Icon: BoltIcon,
  },
  {
    title: "Real-time personalization",
    body: "Sub-100ms parameterized lookups per user, straight from your pipes. No extra service.",
    status: "live",
    Icon: LayersIcon,
  },
  {
    title: "UGC analytics",
    body: "Let creators analyze their own content — per-creator isolation via scoped READ tokens on the same pipes.",
    status: "live",
    Icon: FileCodeIcon,
  },
  {
    title: "Content recommendation",
    body: "Rank and filter candidates with SQL pipes over real-time interaction data. Bring your own scoring.",
    status: "live",
    Icon: TerminalIcon,
  },
  {
    title: "Change data capture",
    body: "Stream a Postgres or Kafka source in via a ClickHouse-native engine declared in your .datasource file.",
    status: "clickhouse",
    note: "Via ClickHouse engine — not yet load-proven",
    Icon: GitBranchIcon,
  },
  {
    title: "Multi-tenant web analytics",
    body: "Traffic analytics with token-scoped reads. One deployment per workspace; scopes isolate within it.",
    status: "partial",
    note: "Single-tenant architecture",
    Icon: ServerIcon,
  },
  {
    title: "Vector search",
    body: "Similarity search over real-time embeddings — passes through to ClickHouse vector functions.",
    status: "clickhouse",
    note: "Passthrough, unverified",
    Icon: PlugIcon,
  },
];

const statusMeta: Record<Status, { label: string; className: string }> = {
  live: {
    label: "Live",
    className: "bg-emerald-500/10 text-emerald-300 ring-emerald-500/20",
  },
  clickhouse: {
    label: "Via ClickHouse",
    className: "bg-amber-400/10 text-amber-300 ring-amber-400/20",
  },
  partial: {
    label: "Partial",
    className: "bg-amber-400/10 text-amber-300 ring-amber-400/20",
  },
};

function StatusBadge({ status }: { status: Status }) {
  const { label, className } = statusMeta[status];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1",
        className,
      )}
    >
      {label}
    </span>
  );
}

// Real receipt from examples/dashboards-demo (ClickHouse 26.6, TinyRaven v0.3.2).
const receipt = [
  { text: "# ingest 300 web events — Tinybird-identical call", variant: "comment" as const },
  { text: '$ curl -X POST ".../v0/events?name=web_events" \\', variant: "default" as const },
  { text: '    --data-binary @events.ndjson', variant: "default" as const },
  { text: '{"quarantined_rows":0,"successful_rows":300}', variant: "added" as const },
  { text: "", variant: "default" as const },
  { text: '# the dashboard\'s "top pages" widget', variant: "comment" as const },
  { text: '$ curl ".../v0/pipes/top_pages.json?limit=5"', variant: "default" as const },
  { text: '{"data":[', variant: "added" as const },
  { text: '  {"path":"/blog/launch","views":82,"visitors":49},', variant: "added" as const },
  { text: '  {"path":"/","views":78,"visitors":52}, ... ],', variant: "added" as const },
  { text: '  "rows":5,"rows_before_limit_at_least":6}', variant: "added" as const },
];

// Live prod endpoints, read-only demo token (scope: READ:top_pages,
// READ:views_over_time — verified 403 on anything else). Public by design.
const DEMO_API = "https://tiny-api.ravencloak.org/v0/pipes";
const DEMO_TOKEN = "tr_rulT-71_qYOtUG-i6MVH9LnKNe5Cucg5";
const liveLinks = [
  {
    label: "top_pages.json?limit=5",
    href: `${DEMO_API}/top_pages.json?limit=5&token=${DEMO_TOKEN}`,
  },
  {
    label: "views_over_time.json",
    href: `${DEMO_API}/views_over_time.json?token=${DEMO_TOKEN}`,
  },
];

export function UseCases() {
  return (
    <section className="mx-auto max-w-6xl px-6 py-24">
      <SectionHeading
        eyebrow="Use cases"
        title="Every Tinybird use case, on infra you own"
        description="Tinybird's documented use cases are patterns over three primitives — ingestion, parameterized SQL pipes, materialized views. TinyRaven has all three. Here's the honest coverage."
      />

      <div className="mt-14 grid gap-px overflow-hidden rounded-2xl border border-white/10 bg-white/10 sm:grid-cols-2 lg:grid-cols-3">
        {useCases.map(({ title, body, status, note, Icon }) => (
          <div key={title} className="group flex flex-col bg-background p-7">
            <div className="flex items-center justify-between">
              <span className="grid h-10 w-10 place-items-center rounded-lg bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/20">
                <Icon className="h-5 w-5" />
              </span>
              <StatusBadge status={status} />
            </div>
            <h3 className="mt-5 text-base font-medium text-zinc-100">{title}</h3>
            <p className="mt-2 flex-1 text-sm leading-relaxed text-zinc-400">
              {body}
            </p>
            {note ? (
              <p className="mt-3 text-xs font-medium text-zinc-500">{note}</p>
            ) : null}
          </div>
        ))}
      </div>

      {/* Dashboards spotlight — proof, not promise */}
      <div className="mt-20 grid items-center gap-10 rounded-2xl border border-white/10 bg-white/[0.02] p-8 lg:grid-cols-2 lg:p-12">
        <div>
          <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-300 ring-1 ring-emerald-500/20">
            Demonstrated
          </span>
          <h3 className="mt-4 text-2xl font-semibold tracking-tight sm:text-3xl">
            User-facing dashboards, drop-in
          </h3>
          <p className="mt-4 text-pretty leading-relaxed text-zinc-400">
            The same <code className="text-violet-300">.datasource</code> and{" "}
            <code className="text-violet-300">.pipe</code> files Tinybird
            consumes, deployed to TinyRaven unchanged. Ingest returns Tinybird&apos;s
            exact response shape; pipes return the full JSON envelope your
            dashboard already parses. Only <code className="text-violet-300">TINYBIRD_HOST</code>{" "}
            changes.
          </p>
          {/* Live, clickable — hits the real prod API with a read-only demo token */}
          <div className="mt-6 rounded-lg border border-white/10 bg-black/20 p-4">
            <p className="text-xs font-medium uppercase tracking-widest text-emerald-300/80">
              Try it live
            </p>
            <div className="mt-3 flex flex-col gap-2">
              {liveLinks.map(({ label, href }) => (
                <a
                  key={label}
                  href={href}
                  target="_blank"
                  rel="noreferrer"
                  className="group flex items-center gap-2 font-mono text-xs text-zinc-300 transition-colors hover:text-violet-300"
                >
                  <span className="text-emerald-400">GET</span>
                  <span className="truncate">{label}</span>
                  <ArrowRightIcon className="h-3 w-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
                </a>
              ))}
            </div>
            <p className="mt-3 text-[11px] text-zinc-500">
              Real prod endpoint · read-only demo token · returns live JSON.
            </p>
          </div>

          <div className="mt-6 flex flex-wrap gap-3">
            <Button
              render={
                <a
                  href={`${links.github}/tree/main/examples/dashboards-demo`}
                  target="_blank"
                  rel="noreferrer"
                />
              }
              size="sm"
              className="bg-white text-zinc-900 hover:bg-zinc-200"
            >
              <GitHubIcon className="h-4 w-4" />
              Reproducible demo
            </Button>
            <Button
              render={<a href={links.github} target="_blank" rel="noreferrer" />}
              size="sm"
              variant="outline"
            >
              Star on GitHub
              <ArrowRightIcon className="h-4 w-4" />
            </Button>
          </div>
        </div>
        <CodeBlock label="dashboards-demo — real output" lines={receipt} />
      </div>
    </section>
  );
}
