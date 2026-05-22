// test-only — never deploy this shape.

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH, loginAsAdmin } from "../fixtures/auth";
import { SEEDED_TUNNEL_NAME } from "../fixtures/env";
import { composeRestartRelay } from "../fixtures/stack";

test.use({ storageState: AUTH_STORAGE_PATH });

test("05-relay-restart: client reconnects after relay container restart", async ({ page }) => {
  test.setTimeout(120_000);

  await page.goto("/tunnels");
  const upstreamRow = page
    .locator('table[aria-label="Tunnels"] tr')
    .filter({ hasText: SEEDED_TUNNEL_NAME });
  await expect(upstreamRow.getByText("connected", { exact: true })).toBeVisible({
    timeout: 15_000,
  });

  // Restart the relay container. blocks until docker compose returns.
  composeRestartRelay();

  // The session cookie may be lost when the relay restarts; if the
  // dashboard redirects to /login, re-login.
  await page.waitForTimeout(2_000);
  if (page.url().includes("/login")) {
    await loginAsAdmin(page);
  } else {
    // Trigger a refresh so the page re-fetches /tunnels and reconnects SSE.
    await page.reload();
  }

  // Within 60s the tunnel must show "connected" again.
  await expect(
    page
      .locator('table[aria-label="Tunnels"] tr')
      .filter({ hasText: SEEDED_TUNNEL_NAME })
      .getByText("connected", { exact: true }),
  ).toBeVisible({ timeout: 60_000 });
});
