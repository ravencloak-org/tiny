"use client";

import { useEffect, useState } from "react";
import { Area } from "@/components/charts/area";
import { AreaChart } from "@/components/charts/area-chart";
import { Bar } from "@/components/charts/bar";
import { BarChart } from "@/components/charts/bar-chart";
import { BarXAxis } from "@/components/charts/bar-x-axis";
import { BarYAxis } from "@/components/charts/bar-y-axis";
import { Grid } from "@/components/charts/grid";
import { XAxis } from "@/components/charts/x-axis";

// Live prod endpoints + read-only demo token (scope READ:top_pages,
// READ:views_over_time — 403 on anything else). Public by design.
const DEMO_API = "https://tiny-api.ravencloak.org/v0/pipes";
const DEMO_TOKEN = "tr_rulT-71_qYOtUG-i6MVH9LnKNe5Cucg5";

type Row = Record<string, unknown>;
type Status = "loading" | "ready" | "error";

async function fetchPipe(pathAndQuery: string): Promise<Row[]> {
  const res = await fetch(`${DEMO_API}/${pathAndQuery}&token=${DEMO_TOKEN}`);
  if (!res.ok) throw new Error(`${pathAndQuery}: ${res.status}`);
  const json = (await res.json()) as { data: Row[] };
  return json.data ?? [];
}

export function LiveDashboard() {
  const [status, setStatus] = useState<Status>("loading");
  const [pages, setPages] = useState<{ name: string; value: number }[]>([]);
  const [series, setSeries] = useState<{ date: Date; value: number }[]>([]);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const [top, hourly] = await Promise.all([
          fetchPipe("top_pages.json?limit=6"),
          fetchPipe("views_over_time.json?"),
        ]);
        if (!alive) return;
        setPages(
          top.map((r) => ({
            name: String(r.path) === "/" ? "home" : String(r.path).replace(/^\//, ""),
            value: Number(r.views),
          })),
        );
        setSeries(
          hourly.map((r) => ({
            // "2026-07-01 10:00:00" -> Date (AreaChart x-axis is time-based)
            date: new Date(String(r.hour).replace(" ", "T")),
            value: Number(r.views),
          })),
        );
        setStatus("ready");
      } catch {
        if (alive) setStatus("error");
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  if (status === "error") {
    return (
      <div className="rounded-2xl border border-white/10 bg-zinc-950/40 p-6 text-sm text-zinc-400">
        Live demo API unreachable right now — raw endpoint:{" "}
        <a
          className="text-violet-300 hover:underline"
          href={`${DEMO_API}/top_pages.json?limit=5&token=${DEMO_TOKEN}`}
          target="_blank"
          rel="noreferrer"
        >
          top_pages.json
        </a>
      </div>
    );
  }

  const loading = status === "loading";

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <ChartCard title="Top pages" unit="views">
        <BarChart
          data={loading ? [] : pages}
          xDataKey="name"
          status={loading ? "loading" : "ready"}
          aspectRatio="16 / 9"
          barGap={0.4}
          margin={{ top: 24, right: 16, bottom: 28, left: 44 }}
        >
          <Grid horizontal numTicksRows={4} />
          <Bar dataKey="value" fill="#a78bfa" lineCap={6} />
          <BarXAxis />
          <BarYAxis maxLabels={5} />
        </BarChart>
      </ChartCard>

      <ChartCard title="Views over time" unit="per hour">
        <AreaChart
          data={loading ? [] : series}
          xDataKey="date"
          status={loading ? "loading" : "ready"}
          aspectRatio="16 / 9"
          margin={{ top: 24, right: 16, bottom: 28, left: 44 }}
        >
          <Grid horizontal numTicksRows={4} />
          <Area dataKey="value" fill="#818cf8" fillOpacity={0.25} strokeWidth={2} />
          <XAxis numTicks={5} />
        </AreaChart>
      </ChartCard>
    </div>
  );
}

function ChartCard({
  title,
  unit,
  children,
}: {
  title: string;
  unit: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-2xl border border-white/10 bg-zinc-950/40 p-5">
      <div className="flex items-baseline justify-between">
        <h4 className="text-sm font-medium text-zinc-100">{title}</h4>
        <span className="text-xs text-zinc-500">{unit}</span>
      </div>
      <div className="mt-2">{children}</div>
    </div>
  );
}
