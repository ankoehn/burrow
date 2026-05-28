// test-only â€” never deploy this shape.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";
import { pingTcpTunnel } from "../../fixtures/traffic";

test.use({ storageState: AUTH_STORAGE_PATH });

test("02-tunnels: tcp-echo bytes counters increment via SSE under traffic", async ({ page, request }) => {
  await page.goto("/tunnels");
  const row = page.locator('table[aria-label="Tunnels"] tr').filter({ hasText: "tcp-echo" });
  await expect(row).toBeVisible();
  // After the IN/OUT merge into TRAFFIC (S6), bytes_in lives in the first
  // <span title="In: ..."> inside .col-traffic.
  const bytesIn = row.locator("td.col-traffic span").filter({ hasText: /^â†“/ });
  const initialIn = (await bytesIn.innerText()).trim();

  await pingTcpTunnel(request, 5);

  await expect.poll(
    async () => (await bytesIn.innerText()).trim(),
    { timeout: 10_000, message: "bytes_in counter did not change under traffic" },
  ).not.toBe(initialIn);
});
