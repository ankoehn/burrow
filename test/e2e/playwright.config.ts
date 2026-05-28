// test-only — never deploy this shape.
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./spec",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://localhost:8080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "mock",
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "postgres",
      use: { ...devices["Desktop Chrome"] },
      grep: /postgres/,
    },
  ],
  outputDir: "./test-results",
});
