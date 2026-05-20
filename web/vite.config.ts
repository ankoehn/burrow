/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import fs from "fs";
import path from "path";

// stripMockServiceWorker removes public/mockServiceWorker.js from the production
// build output. MSW is dev-only (gated behind import.meta.env.DEV in main.tsx and
// dead-code-eliminated from the JS bundle), but the service-worker script is a
// static public asset Vite copies verbatim into dist/. The shipped artifact is
// embedded into burrowd, so it MUST be mock-free; this guarantees every
// `npm run build` produces a clean dist regardless of what is in public/.
function stripMockServiceWorker(): Plugin {
  return {
    name: "strip-mock-service-worker",
    apply: "build",
    closeBundle() {
      const f = path.resolve(__dirname, "dist/mockServiceWorker.js");
      if (fs.existsSync(f)) fs.rmSync(f);
    },
  };
}

export default defineConfig({
  plugins: [react(), stripMockServiceWorker()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: { proxy: { "/api": "http://localhost:8080" } },
  build: { outDir: "dist" },
  // pool:"threads" — Vitest 4's default "forks" pool drops the per-worker
  // state set up before setupFiles run, so any `import { beforeAll } from
  // "vitest"` in setup.ts throws "Vitest failed to access its internal
  // state." on Windows + Node 24. Threads is also deterministically green
  // (the v0.3.0 wall-clock-vs-stability trade), and fileParallelism:false
  // keeps the suite serial to avoid the historical worker-teardown races.
  test: { environment: "jsdom", globals: true, setupFiles: "./src/test/setup.ts", exclude: ["e2e/**", "node_modules/**"], fileParallelism: false, pool: "threads" },
});
