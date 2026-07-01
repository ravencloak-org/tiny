import type { Metadata } from "next";
import { Footer } from "@/components/site/footer";
import { Nav } from "@/components/site/nav";
import { UseCases } from "@/components/site/use-cases";

export const metadata: Metadata = {
  title: "Use cases — TinyRaven",
  description:
    "Every Tinybird use case, self-hosted on infra you own. Honest coverage: user-facing dashboards, gaming analytics, personalization, CDC, and more.",
};

export default function UseCasesPage() {
  return (
    <div className="flex flex-1 flex-col">
      <Nav />
      <main className="flex-1">
        <UseCases />
      </main>
      <Footer />
    </div>
  );
}
