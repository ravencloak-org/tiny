"use client";

import { useEffect, useState } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

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
            hour: String(r.hour).slice(11, 16), // "…10:00:00" -> "10:00"
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

  return (
    <div className="grid gap-6 sm:grid-cols-2">
      <ChartCard title="Top pages" unit="views" loading={status === "loading"}>
        <BarChart data={pages} margin={{ top: 8, right: 8, bottom: 4, left: -16 }}>
          {gridAndAxes("path")}
          <Bar dataKey="views" radius={[4, 4, 0, 0]} maxBarSize={40}>
            {pages.map((_, i) => (
              <Cell key={i} fill="#a78bfa" />
            ))}
          </Bar>
        </BarChart>
      </ChartCard>

      <ChartCard title="Views over time" unit="per hour" loading={status === "loading"}>
        <BarChart data={hourly} margin={{ top: 8, right: 8, bottom: 4, left: -16 }}>
          {gridAndAxes("hour")}
          <Bar dataKey="views" radius={[4, 4, 0, 0]} maxBarSize={28} fill="#818cf8" />
        </BarChart>
      </ChartCard>
    </div>
  );
}

const tick = { fill: "#a1a1aa", fontSize: 11 };

function gridAndAxes(xKey: string) {
  return (
    <>
      <CartesianGrid vertical={false} stroke="rgba(255,255,255,0.06)" />
      <XAxis
        dataKey={xKey}
        tick={tick}
        tickLine={false}
        axisLine={false}
        interval={0}
        angle={xKey === "path" ? -20 : 0}
        textAnchor={xKey === "path" ? "end" : "middle"}
        height={xKey === "path" ? 48 : 24}
      />
      <YAxis tick={tick} tickLine={false} axisLine={false} width={40} allowDecimals={false} />
      <Tooltip
        cursor={{ fill: "rgba(255,255,255,0.04)" }}
        contentStyle={{
          background: "#09090b",
          border: "1px solid rgba(255,255,255,0.1)",
          borderRadius: 8,
          fontSize: 12,
        }}
        labelStyle={{ color: "#e4e4e7" }}
        itemStyle={{ color: "#a78bfa" }}
      />
    </>
  );
}

function ChartCard({
  title,
  unit,
  loading,
  children,
}: {
  title: string;
  unit: string;
  loading: boolean;
  children: React.ReactElement;
}) {
  return (
    <div className="rounded-2xl border border-white/10 bg-zinc-950/40 p-5">
      <div className="flex items-baseline justify-between">
        <h4 className="text-sm font-medium text-zinc-100">{title}</h4>
        <span className="text-xs text-zinc-500">{unit}</span>
      </div>
      <div className="mt-3 h-56">
        {loading ? (
          <div className="h-full w-full animate-pulse rounded-lg bg-white/[0.03]" />
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            {children}
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}
