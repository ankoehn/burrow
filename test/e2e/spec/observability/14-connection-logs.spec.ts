// test-only â€” never deploy this shape.
//
// Plan adaptation: ConnectionLogs is its own page (/connection-logs) â€” there
// isn't a per-service tab in v0.5.2's ServiceDetail. We drive TCP traffic
// against the tcp-echo tunnel, then assert the global /connection-logs
// table has at least one row.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";
import { pingTcpTunnel } from "../../fixtures/traffic";

test.use({ storageState: AUTH_STORAGE_PATH });

test("14-connection-logs: TCP sessions appear in /connection-logs", async ({ page, request }) => {
  await pingTcpTunnel(request, 5);

  await page.goto("/connection-logs");
  await expect(page.getByRole("heading", { name: "Connection logs" })).toBeVisible();

  const table = page.locator('table[aria-label="Connection logs"]').first();
  // At least one row should be present after the 5 fresh-session pings.
  await expect(table.locator("tbody tr").first()).toBeVisible({ timeout: 10_000 });
});
