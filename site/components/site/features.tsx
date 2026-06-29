import type { ComponentType, SVGProps } from "react";
import {
  DatabaseIcon,
  FileCodeIcon,
  GitBranchIcon,
  LayersIcon,
  PlugIcon,
  ServerIcon,
} from "./icons";
import { SectionHeading } from "./section";

type Feature = {
  title: string;
  body: string;
  Icon: ComponentType<SVGProps<SVGSVGElement>>;
};

const features: Feature[] = [
  {
    title: "Drop-in API parity",
    body: "100% Tinybird API surface. Existing client code works by changing only TINYBIRD_HOST — no rewrites, no SDK swaps.",
    Icon: PlugIcon,
  },
  {
    title: ".datasource & .pipe files",
    body: "Define schemas and endpoints as plain files in your repo. Byte-compatible with Tinybird, versioned in Git as the source of truth.",
    Icon: FileCodeIcon,
  },
  {
    title: "Templated pipes → REST",
    body: "Write SQL with {{Type(param)}} templates. TinyRaven validates, escapes, and serves each pipe as a typed JSON REST endpoint.",
    Icon: DatabaseIcon,
  },
  {
    title: "Gatherer batching",
    body: "High-throughput ingest via a goroutine + channel that batches on max(N events, 5s) before flushing to ClickHouse. No tuning required.",
    Icon: LayersIcon,
  },
  {
    title: "tr deploy migrations",
    body: "Branch-aware deploys target one ClickHouse DB per Git branch. Breaking changes go shadow table → MV backfill → atomic EXCHANGE.",
    Icon: GitBranchIcon,
  },
  {
    title: "Self-hosted, your data",
    body: "Runs on your infra against OSS ClickHouse you control. No metering, no egress surprises, no vendor lock-in. Apache 2.0.",
    Icon: ServerIcon,
  },
];

export function Features() {
  return (
    <section id="features" className="mx-auto max-w-6xl scroll-mt-20 px-6 py-24">
      <SectionHeading
        eyebrow="What you get"
        title="Real-time analytics, on your terms"
        description="Everything the Tinybird workflow gives you — files, pipes, deploys — running as a single binary you own."
      />

      <div className="mt-14 grid gap-px overflow-hidden rounded-2xl border border-white/10 bg-white/10 sm:grid-cols-2 lg:grid-cols-3">
        {features.map(({ title, body, Icon }) => (
          <div
            key={title}
            className="group bg-background p-7 transition-colors hover:bg-white/[0.03]"
          >
            <span className="grid h-10 w-10 place-items-center rounded-lg bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/20 transition-colors group-hover:bg-violet-500/15">
              <Icon className="h-5 w-5" />
            </span>
            <h3 className="mt-5 text-base font-medium text-zinc-100">{title}</h3>
            <p className="mt-2 text-sm leading-relaxed text-zinc-400">{body}</p>
          </div>
        ))}
      </div>
    </section>
  );
}
