import { Button } from "@/components/ui/button";
import { links } from "@/lib/data";
import { ArrowRightIcon, GitHubIcon, StarIcon, TerminalIcon } from "./icons";

export function Footer() {
  return (
    <footer className="relative overflow-hidden border-t border-white/5">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_50%_60%_at_50%_120%,rgba(139,92,246,0.16),transparent_70%)]"
      />

      <div className="mx-auto max-w-6xl px-6 py-24">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">
            Own your real-time analytics
          </h2>
          <p className="mx-auto mt-4 max-w-xl text-pretty text-zinc-400">
            One binary. Your servers. The Tinybird API you already write
            against. Clone the repo and run{" "}
            <code className="rounded bg-white/10 px-1.5 py-0.5 font-mono text-sm text-zinc-200">
              tr serve
            </code>
            .
          </p>

          <div className="mt-9 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Button
              render={<a href={links.github} target="_blank" rel="noreferrer" />}
              size="lg"
              className="bg-white text-zinc-900 hover:bg-zinc-200"
            >
              <StarIcon className="h-4 w-4 text-amber-500" />
              Star on GitHub
            </Button>
            <Button
              render={<a href={links.docs} target="_blank" rel="noreferrer" />}
              size="lg"
              variant="outline"
              className="border-white/15 bg-transparent text-zinc-100 hover:bg-white/5 hover:text-white"
            >
              <TerminalIcon className="h-4 w-4" />
              Get started
              <ArrowRightIcon className="h-4 w-4" />
            </Button>
          </div>
        </div>

        <div className="mt-20 flex flex-col items-center justify-between gap-6 border-t border-white/5 pt-8 sm:flex-row">
          <div className="flex items-center gap-2.5">
            <span className="text-sm font-semibold tracking-tight">
              Tiny<span className="text-violet-300">Raven</span>
            </span>
            <span className="text-zinc-600">·</span>
            <span className="text-sm text-zinc-500">Apache 2.0</span>
          </div>

          <nav className="flex items-center gap-6 text-sm text-zinc-400">
            <a
              href={links.github}
              target="_blank"
              rel="noreferrer"
              className="flex items-center gap-1.5 transition-colors hover:text-zinc-100"
            >
              <GitHubIcon className="h-4 w-4" />
              GitHub
            </a>
            <a
              href={links.docs}
              target="_blank"
              rel="noreferrer"
              className="transition-colors hover:text-zinc-100"
            >
              Docs
            </a>
            <span className="text-zinc-600">Built with Go + ClickHouse</span>
          </nav>
        </div>
      </div>
    </footer>
  );
}
