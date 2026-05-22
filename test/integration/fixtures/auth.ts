// test-only — never deploy this shape.

import type { Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD } from "./env";

// Relative to `test/integration/` (where playwright.config.ts lives).
export const AUTH_STORAGE_PATH = "playwright-auth.json";

export async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto("/login");
  await page.fill("#login-email", ADMIN_EMAIL);
  await page.fill("#login-password", ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  // RequireAuth redirects /login → / on success; wait until URL stops being /login.
  await page.waitForURL((u) => !u.pathname.startsWith("/login"), {
    timeout: 10_000,
  });
}
