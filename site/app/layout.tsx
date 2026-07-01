import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "TinyRaven — Tinybird's API, your servers",
  description:
    "Open-source, self-hosted, drop-in alternative to Tinybird. Written in Go over OSS ClickHouse, speaking Tinybird's /v0 API and .datasource/.pipe files. Apache 2.0.",
  metadataBase: new URL("https://tiny.ravencloak.org"),
  openGraph: {
    title: "TinyRaven — Tinybird's API, your servers",
    description:
      "Self-hosted real-time analytics. Drop-in Tinybird /v0 API in a single Go binary. Apache 2.0.",
    type: "website",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`dark ${geistSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col bg-background text-foreground">
        {children}
      </body>
    </html>
  );
}
