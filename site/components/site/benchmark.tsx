"use client";

import { Bar } from "@/components/charts/bar";
import { BarChart } from "@/components/charts/bar-chart";
import { BarXAxis } from "@/components/charts/bar-x-axis";
import { BarYAxis } from "@/components/charts/bar-y-axis";
import { Grid } from "@/components/charts/grid";
import { benchmark } from "@/lib/data";
import { SectionHeading } from "./section";

const tr = benchmark.tinyraven;

const latency = [
  { name: "p50", value: tr.p50ms },
  { name: "p95", value: tr.p95ms },
  { name: "p99", value: tr.p99ms },
];

export function Benchmark() {
  return (
    <section
      id="benchmark"
      className="relative scroll-mt-20 border-t border-white/5 bg-white/[0.02] py-24"
    >
      <div className="mx-auto max-w-6xl px-6">
        <SectionHeading
          eyebrow="Benchmark"
          title="Measured, not marketed"
          description="TinyRaven's own numbers on one commodity node — ingest throughput and end-to-end latency percentiles. No vendor head-to-head; we don't publish numbers we didn't measure."
        />

        <div className="mt-14 grid gap-6 lg:grid-cols-2">
          {/* Throughput — single measured figure */}
          <div className="flex flex-col justify-center rounded-2xl border border-white/10 bg-zinc-950/40 p-8">
            <span className="text-xs font-medium uppercase tracking-widest text-violet-300/80">
              Ingest throughput
            </span>
            <div className="mt-4 flex items-baseline gap-2 font-mono">
              <span className="text-5xl font-semibold text-zinc-100">
                {(tr.throughput / 1e6).toFixed(2)}M
              </span>
              <span className="text-lg text-zinc-500">events / sec</span>
            </div>
            <p className="mt-5 text-sm leading-relaxed text-zinc-400">{tr.note}</p>
          </div>

          {/* Latency percentiles — p50 / p95 / p99 */}
          <div className="rounded-2xl border border-white/10 bg-zinc-950/40 p-6">
            <div className="flex items-baseline justify-between">
              <h3 className="text-base font-medium text-zinc-100">
                Latency percentiles
              </h3>
              <span className="text-xs text-zinc-500">milliseconds</span>
            </div>

            <div className="mt-2">
              <BarChart
                data={latency}
                xDataKey="name"
                aspectRatio="16 / 9"
                barGap={0.5}
                margin={{ top: 24, right: 16, bottom: 28, left: 44 }}
              >
                <Grid horizontal numTicksRows={4} />
                <Bar dataKey="value" fill="#a78bfa" lineCap={6} />
                <BarXAxis />
                <BarYAxis maxLabels={5} />
              </BarChart>
            </div>

            <div className="mt-4 grid grid-cols-3 gap-3">
              {latency.map((d) => (
                <div
                  key={d.name}
                  className="rounded-lg border border-white/5 bg-white/[0.02] px-3 py-2"
                >
                  <div className="text-xs text-zinc-400">{d.name}</div>
                  <div className="mt-1 font-mono text-lg font-semibold text-zinc-100">
                    {d.value} ms
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <p className="mt-8 text-center text-xs text-zinc-500">
          Source: {benchmark.source}
        </p>
      </div>
    </section>
  );
}
