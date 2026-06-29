import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { pricingRows } from "@/lib/data";
import { SectionHeading } from "./section";

export function Pricing() {
  return (
    <section id="pricing" className="mx-auto max-w-6xl scroll-mt-20 px-6 py-24">
      <SectionHeading
        eyebrow="Cost"
        title="Pay for infrastructure, not a tier"
        description="Self-hosting trades a metered SaaS bill for a box you already know how to run. Numbers below are illustrative."
      />

      <div className="mx-auto mt-6 flex justify-center">
        <Badge
          variant="outline"
          className="border-amber-400/30 bg-amber-400/5 text-amber-200/90"
        >
          Illustrative — verify against current Tinybird pricing
        </Badge>
      </div>

      <div className="mx-auto mt-12 max-w-3xl overflow-hidden rounded-2xl border border-white/10 bg-zinc-950/40">
        <Table>
          <TableHeader>
            <TableRow className="border-white/10 hover:bg-transparent">
              <TableHead className="w-[34%] text-zinc-400" />
              <TableHead className="text-zinc-100">
                <div className="flex items-center gap-2 font-semibold">
                  <span className="h-2 w-2 rounded-full bg-violet-400" />
                  TinyRaven
                </div>
              </TableHead>
              <TableHead className="text-zinc-400">
                <div className="flex items-center gap-2 font-medium">
                  <span className="h-2 w-2 rounded-full bg-zinc-500" />
                  Tinybird
                </div>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pricingRows.map((row) => (
              <TableRow key={row.label} className="border-white/5 hover:bg-white/[0.02]">
                <TableCell className="text-sm font-medium text-zinc-400">
                  {row.label}
                </TableCell>
                <TableCell className="text-sm text-zinc-100">
                  {row.tinyraven}
                </TableCell>
                <TableCell className="text-sm text-zinc-400">
                  {row.tinybird}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
