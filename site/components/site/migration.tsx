import { CodeBlock } from "./code-block";
import { ArrowRightIcon } from "./icons";
import { SectionHeading } from "./section";

export function Migration() {
  return (
    <section
      id="migration"
      className="relative scroll-mt-20 border-y border-white/5 bg-white/[0.02] py-24"
    >
      <div className="mx-auto max-w-6xl px-6">
        <SectionHeading
          eyebrow="Migration"
          title="It's one environment variable"
          description="TinyRaven mirrors Tinybird's API exactly. Repoint your client and everything — ingest, pipes, tokens — keeps working."
        />

        <div className="mt-14 grid items-center gap-6 lg:grid-cols-[1fr_auto_1fr]">
          <CodeBlock
            label="before — Tinybird managed"
            lines={[
              { text: "# .env", variant: "comment" },
              { text: "TINYBIRD_HOST=https://api.tinybird.co", variant: "removed" },
              { text: "TINYBIRD_TOKEN=p.eyJ1...", variant: "default" },
            ]}
          />

          <div className="flex justify-center">
            <span className="grid h-10 w-10 place-items-center rounded-full border border-white/10 bg-background text-violet-300 lg:rotate-0">
              <ArrowRightIcon className="h-5 w-5" />
            </span>
          </div>

          <CodeBlock
            label="after — TinyRaven, self-hosted"
            lines={[
              { text: "# .env", variant: "comment" },
              { text: "export TINYBIRD_HOST=https://tiny.ravencloak.org", variant: "added" },
              { text: "TINYBIRD_TOKEN=p.eyJ1...", variant: "default" },
            ]}
          />
        </div>

        <p className="mx-auto mt-8 max-w-xl text-center text-sm text-zinc-500">
          Same endpoints, same file formats, same JSON shapes and error codes.
          The migration is the diff above — nothing else changes.
        </p>
      </div>
    </section>
  );
}
