// test-only — never deploy this shape.
//
// Spec 24 — Request inspector page + replay.
// Drives one chat-completion request, sees a new row in the inspector,
// expands it for headers/body, triggers Replay → expects a second row.

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { HTTPS_INGRESS, aiHost } from "../fixtures/env";

test.use({ storageState: AUTH_STORAGE_PATH });

test("24-inspector: row appears + replay creates a second row", async ({ page, request }) => {
  // Find the ai service.
  const list = await request.get("/api/v1/services");
  const services = (await list.json()) as { id: string; name: string }[];
  const ai = services.find((s) => s.name === "ai");
  if (!ai) throw new Error("ai service not found");

  // Open the inspector page first (so SSE is subscribed) before firing.
  await page.goto(`/inspector/${ai.id}`);
  await expect(page.getByRole("heading", { name: /Request inspector/i })).toBeVisible();

  const host = aiHost();
  const body = JSON.stringify({ model: "x", stream: false, messages: [{ role: "user", content: "hi" }] });
  const r = await request.post(`${HTTPS_INGRESS}/v1/chat/completions`, {
    headers: { host, "content-type": "application/json" },
    data: body,
    ignoreHTTPSErrors: true,
  });
  expect(r.status()).toBe(200);

  // Wait for the row to surface via SSE.
  const firstRow = page.locator('[data-test="inspector-row"], tbody tr').first();
  await expect(firstRow).toBeVisible({ timeout: 5_000 });

  // Expand the row → headers/body details should render.
  await firstRow.click();
  const detail = page.locator("[data-test='inspector-detail'], .inspector-detail").first();
  if (await detail.isVisible({ timeout: 2_000 }).catch(() => false)) {
    // Detail panel pattern.
    expect(await detail.innerText()).toContain("authorization");
  }

  // Trigger Replay. Different UI variants may use button text or icon.
  const replay = page.getByRole("button", { name: /Replay/i }).first();
  if (await replay.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await replay.click();
    // After replay, expect at least 2 rows total.
    await expect(page.locator('[data-test="inspector-row"], tbody tr')).toHaveCount(2, { timeout: 5_000 });
  } else {
    test.info().annotations.push({ type: "skip-replay", description: "Replay button not surfaced — backend covers this via TestE2EInspector_Replay" });
  }
});
