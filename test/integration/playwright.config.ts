// test-only — never deploy this shape.
//
// Plain HTTP baseURL is intentional: burrowd --dev-certs only secures
// the :7000 control plane; the dashboard on :8080 stays plain HTTP.
// See docs/superpowers/specs/2026-05-22-e2e-compose-ui-mini.md §5.

import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./spec",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? "github" : "list",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: "http://localhost:8080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ignoreHTTPSErrors: false,
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
  ],
  outputDir: "./test-results",
});
