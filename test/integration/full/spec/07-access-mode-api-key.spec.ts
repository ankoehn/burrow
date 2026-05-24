// test-only — never deploy this shape.
//
// Plan-fidelity note: the plan drove access-mode config + api-key minting
// through the /tunnels Configure dialog. That path has a real defect in
// v0.5.2 — Tunnels.tsx passes tunnel.id where AccessModePanel/ApiKeysPanel
// expect service.id, so /services/{tunnel-id}/api-keys 404s. The
// architecturally correct UI surface is the per-service detail page at
// /services/<id> → Access + API keys tabs. We use that here. The tunnel-side
// path should be filed as a follow-up defect.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { HTTPS_INGRESS, aiHost } from "../fixtures/env";

test.use({ storageState: AUTH_STORAGE_PATH });

test("07-access-mode-api-key: api_key required; correct key 200; missing 401", async ({ page, request }) => {
  // Plan-fidelity note: /api/v1/internal/test-reset (Plan 1 T18) wipes
  // client_tokens too, which breaks the cached on-disk tokens that
  // relay-full.sh seeded into the client containers. Calling it from inside
  // a Playwright spec leaves the clients unable to re-authenticate until a
  // `docker compose restart`. So this suite does NOT reset between specs;
  // instead specs use unique (timestamp-suffixed) resource names and reset
  // any per-service state explicitly (here: api-key-header field).

  // Resolve the AI service-id via API (Services listing doesn't link to the
  // detail page; ServiceDetail is reachable directly at /services/:id).
  const list = await request.get("/api/v1/services");
  expect(list.status()).toBe(200);
  const services = (await list.json()) as { id: string; name: string }[];
  const ai = services.find((s) => s.name === "ai");
  if (!ai) throw new Error("ai service not found in /api/v1/services");
  await page.goto(`/services/${ai.id}`);
  await expect(page.getByRole("heading", { name: /Service.*\bai\b/ })).toBeVisible();

  // Access tab is the default. Switch the mode to api_key and save.
  await page.getByRole("radio", { name: /API key/ }).click();
  // Explicitly reset the api-key-header input to "Authorization" so a previously
  // typed value doesn't get persisted.
  await page.locator(`#api-key-header-${ai.id}`).fill("Authorization");
  await page.getByRole("button", { name: "Save changes" }).click();
  // Wait for the save mutation to settle (button toggles between Saving… and Save changes).
  await expect(page.getByRole("button", { name: "Save changes" })).toBeEnabled();

  // Now mint a key. The Create key button + dialog live in the same panel.
  await page.getByRole("button", { name: "Create key" }).click();
  const createDialog = page
    .locator('[role="dialog"]', { has: page.getByRole("heading", { name: "Create API key" }) })
    .last();
  await expect(createDialog).toBeVisible();
  await createDialog.locator("#api-key-name").fill(`spec07-${Date.now()}`);
  await createDialog.getByRole("button", { name: "Create", exact: true }).click();

  const revealDialog = page
    .locator('[role="dialog"]', { has: page.getByRole("heading", { name: "Copy your API key now" }) })
    .last();
  await expect(revealDialog).toBeVisible();
  const key = (await revealDialog.locator(".reveal-once .key-row .v").innerText()).trim();
  expect(key.length).toBeGreaterThanOrEqual(16);
  await revealDialog.getByRole("button", { name: "Done" }).click();

  const host = aiHost();
  // With the correct API key (api_key_header defaults to "Authorization"; the
  // proxy strips a "Bearer " prefix for that header — see internal/proxy/access.go).
  const ok = await request.get(`${HTTPS_INGRESS}/healthz`, {
    headers: { host, authorization: `Bearer ${key}` },
    ignoreHTTPSErrors: true,
  });
  expect(ok.status()).toBe(200);

  // Without the API key.
  const denied = await request.get(`${HTTPS_INGRESS}/healthz`, {
    headers: { host },
    ignoreHTTPSErrors: true,
  });
  expect(denied.status()).toBe(401);
});
