// test-only — never deploy this shape.

import { test, expect } from "@playwright/test";
import { loginAsAdmin, AUTH_STORAGE_PATH } from "../fixtures/auth";
import { SEEDED_TUNNEL_NAME } from "../fixtures/env";

test("01-bootstrap: dashboard renders, admin logs in, seeded tunnel is connected", async ({ page, context }) => {
  await loginAsAdmin(page);

  await expect(page.getByRole("heading", { name: "Tunnels" })).toBeVisible();

  const upstreamRow = page
    .locator('table[aria-label="Tunnels"] tr')
    .filter({ hasText: SEEDED_TUNNEL_NAME });
  await expect(upstreamRow).toBeVisible({ timeout: 15_000 });
  await expect(upstreamRow.getByText("connected", { exact: true })).toBeVisible({
    timeout: 15_000,
  });

  // Persist auth for specs 02-05. Path is relative to test/integration/.
  await context.storageState({ path: AUTH_STORAGE_PATH });
});
