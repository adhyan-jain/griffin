import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono, Syne } from "next/font/google";
import { ThemeProvider } from "next-themes";
import { AppShell } from "@/components/app-shell";
import { Toaster } from "@/components/ui/sonner";
import { Cursor } from "@/components/fx/cursor";
import { Intro } from "@/components/fx/intro";
import { SmoothScroll } from "@/components/fx/smooth-scroll";
import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });
// Display face: wide, tightly set, studio-poster energy. Used only for big headlines and numerals.
const syne = Syne({ variable: "--font-syne", subsets: ["latin"], weight: ["600", "700", "800"] });

export const metadata: Metadata = {
  title: "Griffin",
  description: "Your personal AI workforce: hand work to JJ, approve what matters, collect the results.",
  manifest: "/manifest.json",
  icons: { icon: "/favicon.ico", apple: "/icons/apple-touch-icon.png" },
  appleWebApp: { capable: true, title: "Griffin", statusBarStyle: "black-translucent" },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: [
    { media: "(prefers-color-scheme: dark)", color: "#14161c" },
    { media: "(prefers-color-scheme: light)", color: "#fbfaf7" },
  ],
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning className={`${geistSans.variable} ${geistMono.variable} ${syne.variable}`}>
      <body className="min-h-dvh">
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem disableTransitionOnChange>
          <AppShell>{children}</AppShell>
          <Toaster position="top-center" />
          <SmoothScroll />
          <Cursor />
          <Intro />
        </ThemeProvider>
      </body>
    </html>
  );
}
