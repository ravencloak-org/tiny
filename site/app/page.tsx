import { Benchmark } from "@/components/site/benchmark";
import { Features } from "@/components/site/features";
import { Footer } from "@/components/site/footer";
import { Hero } from "@/components/site/hero";
import { Migration } from "@/components/site/migration";
import { Nav } from "@/components/site/nav";
import { Pricing } from "@/components/site/pricing";

export default function Home() {
  return (
    <div className="flex flex-1 flex-col">
      <Nav />
      <main className="flex-1">
        <Hero />
        <Features />
        <Migration />
        <Pricing />
        <Benchmark />
      </main>
      <Footer />
    </div>
  );
}
