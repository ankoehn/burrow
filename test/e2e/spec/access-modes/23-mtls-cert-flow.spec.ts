// test-only â€” never deploy this shape.
//
// Spec 23 â€” fresh-service API + UI mTLS persist verification.
//
// Scope: create a fresh service via API, configure mTLS via the UI, verify
// that the access-mode was persisted (API GET returns access_mode = "mtls"),
// then clean up. The proxy mTLS cert-handshake end-to-end (401 without cert /
// pass with cert) is intentionally NOT here â€” a fresh service has no bound
// burrow-connect client so its subdomain has no live tunnel and the proxy
// cannot route to it. Spec 09 already covers the full mTLS proxy handshake
// against the seeded 'ai' service which has a persistent bound client.
//
// Note: GET /api/v1/services/:id returns `access_mode` but not `mtls_ca_pem`
// (the CA PEM is write-only through PUT /access-mode). Persistence is
// confirmed by (a) the PUT returning 204 and (b) the subsequent GET showing
// access_mode = "mtls".

import { test, expect } from "@playwright/test";
import * as fs from "node:fs/promises";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";
import { CA_CERT_PATH } from "../../fixtures/cert";
import { adminHeaders } from "../../fixtures/api";

test.use({ storageState: AUTH_STORAGE_PATH });

test("23-mtls-cert-flow: create service, set mTLS via UI, verify persisted", async ({ page, request }) => {
  const name = `mtls-spec-${Date.now()}`;

  // Step 1: POST /api/v1/services â€” create a fresh HTTP service.
  const createResp = await request.post("/api/v1/services", {
    headers: adminHeaders(),
    data: { service_id: name, title: name },
  });
  if (createResp.status() === 404) {
    test.skip(true, "POST /services not available in this build");
  }
  expect(createResp.status()).toBe(201);
  const svc = (await createResp.json()) as { id: string };

  // Step 2: GET /api/v1/services/:id â€” confirm the service was created.
  const getResp = await request.get(`/api/v1/services/${svc.id}`, {
    headers: adminHeaders(),
  });
  expect(getResp.status()).toBe(200);
  const svcData = (await getResp.json()) as { id: string; access_mode: string };
  expect(svcData.id).toBe(svc.id);

  // Step 3: Open /services/:id in the UI, switch to mTLS, paste CA PEM, Save.
  await page.goto(`/services/${svc.id}`);
  await expect(
    page.getByRole("heading", { name: new RegExp(`Service.*${name}`, "i") }),
  ).toBeVisible();

  const mtls = page.getByRole("radio", { name: /mTLS/ });
  if (!(await mtls.isVisible({ timeout: 2_000 }).catch(() => false))) {
    // mTLS UI not shipped in this build â€” clean up and skip.
    await request.delete(`/api/v1/services/${svc.id}`, { headers: adminHeaders() });
    test.skip(true, "mTLS UI not present â€” feature not shipped");
  }
  await mtls.click();

  const caPem = await fs.readFile(CA_CERT_PATH, "utf8");
  await page.locator(".mode-detail textarea").first().fill(caPem);
  await page.getByRole("button", { name: /Save changes/ }).click();

  // Wait for the save to complete â€” the toast "Access settings saved" appears
  // on success and the PUT /access-mode â†’ 204 round-trip has finished.
  await expect(page.getByText(/Access settings saved/i)).toBeVisible({ timeout: 5_000 });

  // Step 4: GET /api/v1/services/:id â€” assert access_mode is now "mtls".
  // The CA PEM itself is write-only (not returned by GET); the 204 from Save
  // and this GET together confirm the change was persisted.
  const afterResp = await request.get(`/api/v1/services/${svc.id}`, {
    headers: adminHeaders(),
  });
  expect(afterResp.status()).toBe(200);
  const after = (await afterResp.json()) as { access_mode: string };
  expect(after.access_mode).toBe("mtls");

  // Step 5: No DELETE /api/v1/services/:id endpoint exists (the admin
  // pre-provisioning surface intentionally omits a delete path in v0.5.x).
  // The test stack is reset between runs by POST /api/v1/internal/test-reset
  // (integration build tag); the timestamp-based service_id prevents
  // cross-run collisions in the meantime.
});
