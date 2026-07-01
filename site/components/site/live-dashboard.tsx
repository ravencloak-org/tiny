"use client";

import { useEffect, useState } from "react";
import { Bar } from "@/components/charts/bar";
import { BarChart } from "@/components/charts/bar-chart";
import { BarXAxis } from "@/components/charts/bar-x-axis";
import { BarYAxis } from "@/components/charts/bar-y-axis";
import { Grid } from "@/components/charts/grid";

// Live prod endpoints + read-only demo token (scope READ:top_pages,
// READ:views_over_time — 403 on anything else). Public by design.
const DEMO_API = "https://tiny-api.ravencloak.org/v0/pipes";
const DEMO_TOKEN = "tr_rulT-71_qYOtUG-i6MVH9LnKNe5Cucg5";

type Row = Record<string, unknown>;
type Status = "loading" | "ready" | "error";

async function fetchPipe(path: string): Promise<Row[]> {
  const res = await fetch(`${DEMO_API}/${path}&token=${DEMO_TOKEN}`);
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  const json = (await res.json()) as { data: Row[] };
  return json.data ?? [];
}

export function LiveDashboard() {
  const [status, setStatus] = useState<Status>("loading");
  const [pages, setPages] = useState<{ path: string; views: number }[]>([]);
  const [hourly, setHourly] = useState<{ hour: string; views: number }[]>([]);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const [top, series] = await Promise.all([
          fetchPipe("top_pages.json?limit=6"),
          fetchPipe("views_over_time.json?"),
        ]);
        if (!alive) return;
        setPages(
          top.map((r) => ({ path: String(r.path), views: Number(r.views) })),
        );
        setHourly(
          series.map((r) => ({
            // "2026-07-01 10:00:00" -> "10:00"
            hour: String(r.hour).slice(11, 16),
            views: Number(r.views),
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
        Live demo API unreachable right now. Raw endpoints:{" "}
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

  return (
    <div className="grid gap-6 sm:grid-cols-2">
      <ChartCard title="Top pages" unit="views">
        <BarChart
          data={status === "loading" ? [] : pages}
          xDataKey="path"
          status={status === "loading" ? "loading" : "ready"}
          aspectRatio="16 / 10"
          barGap={0.35}
          margin={{ top: 20, right: 12, bottom: 40, left: 40 }}
        >
          <Grid horizontal numTicksRows={4} />
          <Bar dataKey="views" fill="#a78bfa" lineCap={6} />
          <BarXAxis />
          <BarYAxis maxLabels={4} />
        </BarChart>
      </ChartCard>

      <ChartCard title="Views over time" unit="per hour">
        <BarChart
          data={status === "loading" ? [] : hourly}
          xDataKey="hour"
          status={status === "loading" ? "loading" : "ready"}
          aspectRatio="16 / 10"
          barGap={0.25}
          margin={{ top: 20, right: 12, bottom: 40, left: 40 }}
        >
          <Grid horizontal numTicksRows={4} />
          <Bar dataKey="views" fill="#818cf8" lineCap={6} />
          <BarXAxis />
          <BarYAxis maxLabels={4} />
        </BarChart>
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
