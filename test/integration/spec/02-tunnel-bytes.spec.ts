// test-only — never deploy this shape.

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { SEEDED_TUNNEL_NAME, TUNNEL_URL } from "../fixtures/env";

test.use({ storageState: AUTH_STORAGE_PATH });

test("02-tunnel-bytes: bytes counters increment via SSE when traffic flows", async ({ page, request }) => {
  await page.goto("/tunnels");

  const upstreamRow = page
    .locator('table[aria-label="Tunnels"] tr')
    .filter({ hasText: SEEDED_TUNNEL_NAME });
  await expect(upstreamRow).toBeVisible({ timeout: 10_000 });

  // Capture initial bytes_in text (e.g. "0 B" or "1.2 KiB").
  const bytesInCell = upstreamRow.locator("td.col-bytes").nth(0);
  const bytesOutCell = upstreamRow.locator("td.col-bytes").nth(1);
  const initialIn = (await bytesInCell.innerText()).trim();
  const initialOut = (await bytesOutCell.innerText()).trim();

  // Drive 5 POSTs with ~4 KiB bodies each (~20 KiB total) so the formatted
  // counter value always changes regardless of the current KiB/MiB scale.
  // Small payloads (< 200 B) can round to the same "X.Y KiB" string.
  //
  // "connection: close" is required: bridge.Pipe counts bytes only after
  // io.Copy returns (i.e. when the TCP connection closes). Without it,
  // HTTP/1.1 keep-alive holds the connection open and the relay never
  // flushes the byte counter during the test window.
  const padding = "x".repeat(4000);
  for (let i = 0; i < 5; i++) {
    const res = await request.post(`${TUNNEL_URL}/echo`, {
      data: { spec: 2, iter: i, pad: padding },
      headers: { "content-type": "application/json", "connection": "close" },
    });
    expect(res.status()).toBe(200);
  }

  // Within 10s, both counters must change. SSE event "tunnels" fires
  // on each tunnel-stats update; the page useQuery invalidates
  // ["tunnels"] and re-fetches.
  await expect
    .poll(async () => (await bytesInCell.innerText()).trim(), {
      timeout: 10_000,
      message: 'bytes_in counter did not change within 10s — SSE "tunnels" event may not be firing',
    })
    .not.toBe(initialIn);

  await expect
    .poll(async () => (await bytesOutCell.innerText()).trim(), {
      timeout: 10_000,
      message: "bytes_out counter did not change within 10s",
    })
    .not.toBe(initialOut);
});
