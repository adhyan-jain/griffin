import type { NextConfig } from "next";

// The API's CORS allowlist is a strict exact-match list containing only this origin. Any other
// hostname that serves the app (the per-deployment URL Vercel prints after a deploy, the team
// alias, branch aliases) would be rejected by the API at preflight, so production builds send
// visitors to the canonical host (set CANONICAL_HOST, e.g. your-app.vercel.app) instead of leaving
// them on an origin that cannot work. Unset: no canonical redirect.
const CANONICAL_HOST = process.env.CANONICAL_HOST;
const escapeRegex = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const nextConfig: NextConfig = {
  // Lets check:bundle build into a throwaway directory without clobbering .next.
  ...(process.env.NEXT_DIST_DIR ? { distDir: process.env.NEXT_DIST_DIR } : {}),
  async redirects() {
    if (process.env.VERCEL_ENV !== "production" || !CANONICAL_HOST) return [];
    return [
      {
        source: "/:path*",
        // Every host except the canonical one (and localhost, for `next start` checks).
        has: [{ type: "host", value: `(?!(?:${escapeRegex(CANONICAL_HOST)}|localhost(?::\\d+)?)$).+` }],
        destination: `https://${CANONICAL_HOST}/:path*`,
        permanent: false,
      },
    ];
  },
  // Always define the dev-login flag at build time ("false" unless explicitly enabled) so it is
  // inlined as a constant and the dev-login code is stripped from production bundles.
  env: {
    NEXT_PUBLIC_ENABLE_DEV_LOGIN: process.env.NEXT_PUBLIC_ENABLE_DEV_LOGIN === "true" ? "true" : "false",
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
