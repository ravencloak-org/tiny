import { cn } from "@/lib/utils";

interface CodeLine {
  text: string;
  // visual treatment for diff-style blocks
  variant?: "default" | "added" | "removed" | "comment";
}

interface CodeBlockProps {
  lines: CodeLine[];
  label?: string;
  className?: string;
}

const variantClass: Record<NonNullable<CodeLine["variant"]>, string> = {
  default: "text-zinc-200",
  added: "text-emerald-300",
  removed: "text-rose-300/70 line-through decoration-rose-400/40",
  comment: "text-zinc-500",
};

export function CodeBlock({ lines, label, className }: CodeBlockProps) {
  return (
    <div
      className={cn(
        "overflow-hidden rounded-xl border border-white/10 bg-zinc-950/80 shadow-2xl shadow-black/40 backdrop-blur",
        className,
      )}
    >
      <div className="flex items-center gap-2 border-b border-white/5 px-4 py-2.5">
        <span className="h-3 w-3 rounded-full bg-rose-500/70" />
        <span className="h-3 w-3 rounded-full bg-amber-400/70" />
        <span className="h-3 w-3 rounded-full bg-emerald-500/70" />
        {label ? (
          <span className="ml-2 font-mono text-xs text-zinc-500">{label}</span>
        ) : null}
      </div>
      <pre className="overflow-x-auto px-4 py-4 font-mono text-[13px] leading-relaxed">
        <code>
          {lines.map((line, i) => (
            <div key={i} className={variantClass[line.variant ?? "default"]}>
              {line.text || " "}
            </div>
          ))}
        </code>
      </pre>
    </div>
  );
}
