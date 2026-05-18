/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: { proxy: { "/api": "http://localhost:8080" } },
  build: { outDir: "dist" },
  test: { environment: "jsdom", globals: true, setupFiles: "./src/test/setup.ts", exclude: ["e2e/**", "node_modules/**"] },
});
