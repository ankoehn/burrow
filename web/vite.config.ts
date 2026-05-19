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
  // fileParallelism:false — the suite is timing-flaky under the parallel
  // forks pool (worker-teardown races surface as sporadic, file-order-
  // dependent failures); it is deterministically green run serially. Trades
  // a little wall-clock for a trustworthy gate.
  test: { environment: "jsdom", globals: true, setupFiles: "./src/test/setup.ts", exclude: ["e2e/**", "node_modules/**"], fileParallelism: false },
});
