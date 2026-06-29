import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Static marketing site — export to plain HTML/JS, served by nginx.
  output: "export",
  images: { unoptimized: true },
};

export default nextConfig;
