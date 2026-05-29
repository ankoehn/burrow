// test-only — never deploy this shape.
// Runs ONLY in the `postgres` Playwright project, selected by `testMatch` on
// this filename in playwright.config.ts. Self-authenticates via loginAsAdmin,
// so it needs no bootstrap-seeded storageState.
//
// Assumes the stack is up with the Postgres override:
//   docker compose -f compose.full.yml -f compose.full.postgres.yml up -d --build --wait
import { test, expect } from "@playwright/test";
import { loginAsAdmin } from "../../fixtures/auth";
import { TUNNEL_NAMES } from "../../fixtures/env";

test("19-postgres: dashboard + tunnels work identically on Postgres backend", async ({ page }) => {
  await loginAsAdmin(page);
  await page.goto("/tunnels");
  await expect(page.getByRole("heading", { name: "Tunnels" })).toBeVisible();
  for (const name of TUNNEL_NAMES) {
    await expect(
      page.locator('table[aria-label="Tunnels"] tr').filter({ hasText: name }),
    ).toBeVisible({ timeout: 15_000 });
  }
});
