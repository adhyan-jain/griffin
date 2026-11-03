import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Lets check:bundle build into a throwaway directory without clobbering .next.
  ...(process.env.NEXT_DIST_DIR ? { distDir: process.env.NEXT_DIST_DIR } : {}),
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
