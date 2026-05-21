// global-setup.ts — runs ONCE before any spec; authenticates the seeded
// admin via the JSON API and persists session cookies to
// `web/playwright-auth.json`. v0.4.0 specs that need an authenticated admin
// opt in via test.use({ storageState: ... }) — collapsing ~30 logins down
// to a single one and keeping the suite well under the per-IP login rate
// limit (LoginRateLimitPerIP=10/min in internal/api/deps.go).

import { chromium, request, type FullConfig } from "@playwright/test";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const ADMIN_EMAIL = "e2e@example.com";
const ADMIN_PASSWORD = "e2e-password-123";

const _dirname = path.dirname(fileURLToPath(import.meta.url));
export const AUTH_STATE_FILE = path.join(_dirname, "..", "playwright-auth.json");

export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use.baseURL;
  if (!baseURL) throw new Error("baseURL is not configured");

  // 1. Issue ONE login via the JSON API (no UI flow needed).
  const apiCtx = await request.newContext({ baseURL });
  const res = await apiCtx.post("/api/v1/auth/login", {
    headers: { "Content-Type": "application/json" },
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  });
  if (!res.ok()) {
    throw new Error(
      `global-setup login failed: ${res.status()} ${await res.text()}`,
    );
  }
  const cookies = await apiCtx.storageState();
  await apiCtx.dispose();

  // 2. Round-trip through a real browser context to write storageState in
  //    the exact shape Playwright re-reads. /me must come back 200.
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ baseURL, storageState: cookies });
  const page = await ctx.newPage();
  const meRes = await page.goto("/api/v1/me");
  if (!meRes || meRes.status() !== 200) {
    throw new Error(`global-setup /me returned ${meRes?.status() ?? "no-response"}`);
  }
  await ctx.storageState({ path: AUTH_STATE_FILE });
  await browser.close();
}
