import { Button } from "@/components/ui/button";
import { links } from "@/lib/data";
import { CodeBlock } from "./code-block";
import { ArrowRightIcon, GitHubIcon } from "./icons";

export function Hero() {
  return (
    <section id="top" className="relative overflow-hidden">
      {/* ambient glow + grid */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_60%_50%_at_50%_-10%,rgba(139,92,246,0.18),transparent_70%)]"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[linear-gradient(to_right,rgba(255,255,255,0.04)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.04)_1px,transparent_1px)] bg-[size:48px_48px] [mask-image:radial-gradient(ellipse_70%_60%_at_50%_0%,black,transparent_75%)]"
      />

      <div className="mx-auto max-w-6xl px-6 pb-20 pt-20 md:pt-28">
        <div className="mx-auto max-w-3xl text-center">
          <a
            href={links.github}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/5 px-3.5 py-1.5 text-xs text-zinc-300 transition-colors hover:border-white/20 hover:text-white"
          >
            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
            Open source · Apache 2.0
          </a>

          <h1 className="mt-6 text-balance text-4xl font-semibold tracking-tight sm:text-5xl md:text-6xl">
            Tinybird&apos;s API,{" "}
            <span className="bg-gradient-to-r from-violet-300 via-violet-400 to-indigo-300 bg-clip-text text-transparent">
              your servers.
            </span>
          </h1>

          <p className="mx-auto mt-6 max-w-2xl text-pretty text-lg leading-relaxed text-zinc-400">
            TinyRaven is a self-hosted, drop-in alternative to Tinybird —
            written in Go over OSS ClickHouse with 100% API parity. Point your
            existing client at a new host and keep shipping.
          </p>

          <div className="mt-9 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Button
              render={<a href={links.github} target="_blank" rel="noreferrer" />}
              size="lg"
              className="bg-white text-zinc-900 hover:bg-zinc-200"
            >
              <GitHubIcon className="h-4 w-4" />
              Star on GitHub
            </Button>
            <Button
              render={<a href={links.docs} target="_blank" rel="noreferrer" />}
              size="lg"
              variant="outline"
              className="border-white/15 bg-transparent text-zinc-100 hover:bg-white/5 hover:text-white"
            >
              Read the docs
              <ArrowRightIcon className="h-4 w-4" />
            </Button>
          </div>

          <div className="mt-7 flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-sm text-zinc-500">
            <span>Single Go binary</span>
            <span className="hidden h-1 w-1 rounded-full bg-zinc-700 sm:inline-block" />
            <span>ClickHouse-powered</span>
            <span className="hidden h-1 w-1 rounded-full bg-zinc-700 sm:inline-block" />
            <span>API-first, no lock-in</span>
          </div>
        </div>

        <div className="mx-auto mt-14 max-w-2xl">
          <CodeBlock
            label="ingest + query — same API as Tinybird"
            lines={[
              { text: "# stream events in", variant: "comment" },
              { text: "curl -X POST $TINYBIRD_HOST/v0/events \\", variant: "default" },
              { text: '  -H "Authorization: Bearer $TOKEN" \\', variant: "default" },
              { text: "  -d '{\"event\":\"pageview\",\"path\":\"/\"}'", variant: "default" },
              { text: "", variant: "default" },
              { text: "# query a pipe as REST", variant: "comment" },
              { text: "curl $TINYBIRD_HOST/v0/pipes/top_pages.json?limit=10", variant: "default" },
            ]}
          />
        </div>
      </div>
    </section>
  );
}
