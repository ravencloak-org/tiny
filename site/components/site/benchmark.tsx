"use client";

import { Bar } from "@/components/charts/bar";
import { BarChart } from "@/components/charts/bar-chart";
import { BarXAxis } from "@/components/charts/bar-x-axis";
import { BarYAxis } from "@/components/charts/bar-y-axis";
import { Grid } from "@/components/charts/grid";
import { benchmark } from "@/lib/data";
import { SectionHeading } from "./section";

const throughputData = [
  { name: "TinyRaven", value: benchmark.tinyraven.throughput },
  { name: "Tinybird", value: benchmark.tinybird.throughput },
];

const latencyData = [
  { name: "TinyRaven", value: benchmark.tinyraven.p95ms },
  { name: "Tinybird", value: benchmark.tinybird.p95ms },
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
          title="Parity on performance, too"
          description="Throughput and p95 latency, measured side by side. TinyRaven keeps pace with managed Tinybird on commodity hardware."
        />

        <div className="mt-14 grid gap-6 lg:grid-cols-2">
          <ChartCard
            title="Throughput"
            unit="events / sec"
            data={throughputData}
            fill="#a78bfa"
            format={(v) => `${(v / 1000).toFixed(0)}k`}
            note={`${benchmark.tinyraven.throughput.toLocaleString()} ev/s · ${benchmark.tinyraven.note}`}
          />
          <ChartCard
            title="p95 latency"
            unit="milliseconds (lower is better)"
            data={latencyData}
            fill="#818cf8"
            format={(v) => `${v} ms`}
            note={`TinyRaven ${benchmark.tinyraven.p95ms} ms vs Tinybird ${benchmark.tinybird.p95ms} ms`}
          />
        </div>

        <p className="mt-8 text-center text-xs text-zinc-500">
          Source: {benchmark.source}
        </p>
      </div>
    </section>
  );
}

interface ChartCardProps {
  title: string;
  unit: string;
  data: { name: string; value: number }[];
  fill: string;
  format: (v: number) => string;
  note: string;
}

function ChartCard({ title, unit, data, fill, format, note }: ChartCardProps) {
  return (
    <div className="rounded-2xl border border-white/10 bg-zinc-950/40 p-6">
      <div className="flex items-baseline justify-between">
        <h3 className="text-base font-medium text-zinc-100">{title}</h3>
        <span className="text-xs text-zinc-500">{unit}</span>
      </div>

      <div className="mt-2">
        <BarChart
          data={data}
          xDataKey="name"
          aspectRatio="16 / 9"
          barGap={0.45}
          margin={{ top: 24, right: 16, bottom: 28, left: 44 }}
        >
          <Grid horizontal numTicksRows={4} />
          <Bar dataKey="value" fill={fill} lineCap={6} />
          <BarXAxis />
          <BarYAxis maxLabels={5} />
        </BarChart>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-3">
        {data.map((d, i) => (
          <div
            key={d.name}
            className="rounded-lg border border-white/5 bg-white/[0.02] px-3 py-2"
          >
            <div className="flex items-center gap-2 text-xs text-zinc-400">
              <span
                className="h-2 w-2 rounded-full"
                style={{ background: i === 0 ? fill : "#52525b" }}
              />
              {d.name}
            </div>
            <div className="mt-1 font-mono text-lg font-semibold text-zinc-100">
              {format(d.value)}
            </div>
          </div>
        ))}
      </div>

      <p className="mt-4 text-xs text-zinc-500">{note}</p>
    </div>
  );
}
