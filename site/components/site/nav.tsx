import Link from "next/link";
import { Button } from "@/components/ui/button";
import { links } from "@/lib/data";
import { GitHubIcon } from "./icons";

const navItems = [
  { label: "Features", href: "/#features" },
  { label: "Use cases", href: "/use-cases" },
  { label: "Migration", href: "/#migration" },
  { label: "Pricing", href: "/#pricing" },
  { label: "Benchmark", href: "/#benchmark" },
];

export function Nav() {
  return (
    <header className="sticky top-0 z-50 border-b border-white/5 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
        <Link href="#top" className="flex items-center gap-2.5">
          <span className="grid h-8 w-8 place-items-center rounded-lg bg-violet-500/15 text-violet-300 ring-1 ring-violet-500/30">
            <RavenMark className="h-5 w-5" />
          </span>
          <span className="text-[15px] font-semibold tracking-tight">
            Tiny<span className="text-violet-300">Raven</span>
          </span>
        </Link>

        <nav className="hidden items-center gap-7 md:flex">
          {navItems.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className="text-sm text-zinc-400 transition-colors hover:text-zinc-100"
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <div className="flex items-center gap-2">
          <Button
            render={<a href={links.github} target="_blank" rel="noreferrer" />}
            size="sm"
            className="bg-white text-zinc-900 hover:bg-zinc-200"
          >
            <GitHubIcon className="h-4 w-4" />
            GitHub
          </Button>
        </div>
      </div>
    </header>
  );
}

function RavenMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M3 6c4 0 7 1.5 9 4 1.2-1 2.8-1.5 4.5-1.5L21 5l-1.5 4c.9 1 1.5 2.4 1.5 4 0 4-3.5 7-9 7-3 0-5.4-1-7-2.6 1.2.3 2.4.2 3.4-.3C5 18 3.5 15.5 3.5 12c0-.8.1-1.5.3-2.2C3.3 9 3 7.7 3 6Zm12 4.5a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z" />
    </svg>
  );
}
