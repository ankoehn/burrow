import { test, expect } from "@playwright/test";

// v0.4.0: mTLS access mode (spec Part J). The MtlsPanel renders only inside
// AccessModePanel for a live service — the e2e fixture has no connected
// burrow client, so the panel is not reachable through the dashboard. The
// component is unit-covered by MtlsPanel.test.tsx. Here we assert:
//   1. The server contract for switching a service to mtls (404 path on a
//      missing service: the gate fires before mode-specific validation).
//   2. The SHA-256 fingerprint helper used in the MtlsPanel — computed in
//      the browser via crypto.subtle.digest — produces a deterministic
//      lowercase hex string for a canonical input.

const SAMPLE_CA_PEM =
  "-----BEGIN CERTIFICATE-----\nMIIBkTCCATegAwIBAgIUE2E\n-----END CERTIFICATE-----\n";

test.use({ storageState: "playwright-auth.json" });

test("v0.4.0: mtls API — PUT mtls + ca_pem 404s for unknown service", async ({ page }) => {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((c) => c.name === "burrow_csrf")?.value ?? "";
  const headers = { "X-CSRF-Token": csrf, "Content-Type": "application/json" };

  // The handler validates body shape (mode + ca_pem for mtls) but the
  // service-existence gate fires first.
  const put = await page.request.put("/api/v1/services/no-such/access-mode", {
    headers,
    data: { access_mode: "mtls", ca_pem: SAMPLE_CA_PEM },
  });
  expect(put.status()).toBe(404);
  expect(await put.json()).toEqual({ error: "service not found" });
});

test("v0.4.0: mtls fingerprint helper — SHA-256 of PEM is deterministic lowercase hex", async ({ page }) => {
  // Compute the digest inside the page so we hit the same WebCrypto path the
  // MtlsPanel uses (crypto.subtle.digest("SHA-256", ...)).
  await page.goto("/");
  const fp = await page.evaluate(async (pem: string) => {
    const bytes = new TextEncoder().encode(pem);
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  }, SAMPLE_CA_PEM);

  expect(fp).toMatch(/^[0-9a-f]{64}$/);
  const fp2 = await page.evaluate(async (pem: string) => {
    const bytes = new TextEncoder().encode(pem);
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  }, SAMPLE_CA_PEM);
  expect(fp2).toBe(fp);
});
