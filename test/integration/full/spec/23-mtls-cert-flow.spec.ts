// test-only — never deploy this shape.
//
// Spec 23 — fresh-service mTLS flow.
// Creates a new HTTP service in-test, configures mTLS via the UI, then
// asserts the full cert-roundtrip works. Validates create-service →
// upload-CA → cert-handshake end-to-end (distinct from spec 09 which
// uses the seeded 'ai' service).

import { test, expect, request as pwRequest } from "@playwright/test";
import * as fs from "node:fs/promises";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";
import { HTTPS_INGRESS } from "../fixtures/env";
import { CLIENT_CERT_PATH, CLIENT_KEY_PATH, CA_CERT_PATH } from "../fixtures/cert";
import { adminHeaders } from "../fixtures/api";

test.use({ storageState: AUTH_STORAGE_PATH });

test("23-mtls-cert-flow: create service, set mTLS, cert handshake", async ({ page, request }) => {
  const name = `mtls-spec-${Date.now()}`;

  // 1. Create a new HTTP service via the admin API.
  // POST /api/v1/services expects { service_id, title?, access_mode? }.
  // The response is { id, created_at }; subdomain is empty until a client
  // connects — use `name` (the service_id) as the subdomain for routing.
  const createResp = await request.post("/api/v1/services", {
    headers: adminHeaders(),
    data: { service_id: name, title: name },
  });
  if (createResp.status() === 404) {
    test.skip(true, "POST /services not available in this build");
  }
  expect(createResp.status()).toBe(201);
  const svc = (await createResp.json()) as { id: string };
  const subdomain = name; // service_id doubles as subdomain in the test stack

  // 2. Open the service detail page, switch to mTLS, upload CA.
  await page.goto(`/services/${svc.id}`);
  await expect(page.getByRole("heading", { name: new RegExp(`Service.*${name}`) })).toBeVisible();
  const mtls = page.getByRole("radio", { name: /mTLS/ });
  if (!(await mtls.isVisible({ timeout: 2_000 }).catch(() => false))) {
    test.skip(true, "mTLS UI not present — feature not shipped");
  }
  await mtls.click();
  const caPem = await fs.readFile(CA_CERT_PATH, "utf8");
  await page.locator(".mode-detail textarea").first().fill(caPem);
  await page.getByRole("button", { name: /Save changes/ }).click();
  await page.waitForTimeout(500);

  // 3. The new service has NO live client tunnel, so /healthz on its host
  //    will 502, but the mTLS gate fires FIRST. Without cert → 401.
  const host = `${subdomain}.test.local:8443`;
  const denied = await request.get(`${HTTPS_INGRESS}/healthz`, {
    headers: { host },
    ignoreHTTPSErrors: true,
  });
  expect(denied.status()).toBe(401);

  // 4. With cert → mTLS gate passes; proxy responds with 502 (no tunnel) OR
  //    a clear error, NOT a 401. The point is the gate let us through.
  const certCtx = await pwRequest.newContext({
    ignoreHTTPSErrors: true,
    clientCertificates: [{
      origin: HTTPS_INGRESS,
      certPath: CLIENT_CERT_PATH,
      keyPath: CLIENT_KEY_PATH,
    }],
  });
  const past = await certCtx.get(`${HTTPS_INGRESS}/healthz`, { headers: { host } });
  expect(past.status()).not.toBe(401);
  await certCtx.dispose();

  // 5. Cleanup: delete the test service.
  await request.delete(`/api/v1/services/${svc.id}`, { headers: adminHeaders() });
});
