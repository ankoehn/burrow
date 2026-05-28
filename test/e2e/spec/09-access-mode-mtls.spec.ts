// test-only — never deploy this shape.
//
// v0.4 mTLS access-mode end-to-end. Replaces the prior "UI surface only"
// version (Playwright 1.60+ supports per-request clientCertificates, so
// we can do a real cert handshake from spec).
//
// Flow:
//   1. UI: open /services/<ai-id>, switch to mTLS mode, paste test CA pem, save.
//   2. Backend: hit https://<sub>.test.local:8443/healthz from a new request
//      context WITH clientCertificates → expect 200.
//   3. Same without → expect 401.
//   4. Reset to Open before exit so other specs aren't affected.

import { test, expect, request as pwRequest } from "@playwright/test";
import * as fs from "node:fs/promises";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { HTTPS_INGRESS, aiHost } from "../fixtures/env";
import { CLIENT_CERT_PATH, CLIENT_KEY_PATH, CA_CERT_PATH } from "../fixtures/cert";

test.use({ storageState: AUTH_STORAGE_PATH });

test("09-access-mode-mtls: real cert handshake gates proxy access", async ({ page, request }) => {
  // 1. Find the ai service and open its detail page.
  const list = await request.get("/api/v1/services");
  const services = (await list.json()) as { id: string; name: string }[];
  const ai = services.find((s) => s.name === "ai");
  if (!ai) throw new Error("ai service not found");
  await page.goto(`/services/${ai.id}`);
  await expect(page.getByRole("heading", { name: /Service.*\bai\b/ })).toBeVisible();

  // 2. Switch to mTLS, paste the test CA pem.
  const mtls = page.getByRole("radio", { name: /mTLS/ });
  await expect(mtls).toBeVisible({ timeout: 2_000 });
  await mtls.click();

  const caPem = await fs.readFile(CA_CERT_PATH, "utf8");
  const textarea = page.locator(".mode-detail textarea").first();
  await expect(textarea).toBeVisible();
  await textarea.fill(caPem);
  await page.getByRole("button", { name: /Save changes/ }).click();

  // Wait for the save toast or the dialog to close (depending on layout).
  await page.waitForTimeout(500);

  const host = aiHost(); // e.g., "xxx.test.local:8443"

  // 3. Without cert → 401.
  const denied = await request.get(`${HTTPS_INGRESS}/healthz`, {
    headers: { host },
    ignoreHTTPSErrors: true,
  });
  expect(denied.status()).toBe(401);

  // 4. With valid client cert → 200.
  const certCtx = await pwRequest.newContext({
    ignoreHTTPSErrors: true,
    clientCertificates: [{
      origin: HTTPS_INGRESS,
      certPath: CLIENT_CERT_PATH,
      keyPath: CLIENT_KEY_PATH,
    }],
  });
  const ok = await certCtx.get(`${HTTPS_INGRESS}/healthz`, {
    headers: { host },
  });
  expect(ok.status()).toBe(200);
  await certCtx.dispose();

  // 5. Reset access mode to Open so subsequent specs aren't affected.
  await page.getByRole("radio", { name: /Open/ }).click();
  await page.getByRole("button", { name: /Save changes/ }).click();
});
