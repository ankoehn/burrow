// test-only — never deploy this shape.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { pingTcpTunnel } from "../fixtures/traffic";

test.use({ storageState: AUTH_STORAGE_PATH });

test("02-tunnels: tcp-echo bytes counters increment via SSE under traffic", async ({ page, request }) => {
  await page.goto("/tunnels");
  const row = page.locator('table[aria-label="Tunnels"] tr').filter({ hasText: "tcp-echo" });
  await expect(row).toBeVisible();
  const initialIn = (await row.locator("td.col-bytes").nth(0).innerText()).trim();

  await pingTcpTunnel(request, 5);

  await expect.poll(
    async () => (await row.locator("td.col-bytes").nth(0).innerText()).trim(),
    { timeout: 10_000, message: "bytes_in counter did not change under traffic" },
  ).not.toBe(initialIn);
});
