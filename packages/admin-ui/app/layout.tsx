import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Dominion Admin",
  description: "Governance spine for agentic AI in the enterprise.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
