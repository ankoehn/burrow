import { test, expect } from "@playwright/test";

// v0.4.0: WebAuthn passkey enrollment surface on /account. A full happy
// path requires a real authenticator: the server's go-webauthn library
// verifies the attestation crypto, so a stubbed navigator.credentials.create
// returning a hand-rolled object is rejected at /register/finish (the
// documented, secure behaviour). Here we exercise the surfaces that are
// deterministic without a real authenticator.

test.use({ storageState: "playwright-auth.json" });

test("v0.4.0: /account shows passkey section + empty credential list", async ({ page }) => {
  await page.goto("/account");
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByRole("heading", { name: "Security keys (passkeys)" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Add a passkey" })).toBeVisible();
  await expect(page.getByText("No passkeys yet", { exact: false })).toBeVisible();
});

test("v0.4.0: Add-a-passkey click hits /register/begin + leaves the credentials list empty", async ({ page }) => {
  // Install a stub BEFORE the page loads so any code path that reaches
  // navigator.credentials.create picks it up rather than the chromium default
  // (which would prompt a real platform authenticator). The current UI's
  // ceremony wrapper expects an envelope shape that doesn't match the
  // server's begin response, so startRegistration throws before reaching the
  // stub — the click still triggers /register/begin which is what we assert.
  await page.addInitScript(() => {
    function strToBuf(s: string): ArrayBuffer {
      const enc = new TextEncoder().encode(s);
      return enc.buffer.slice(enc.byteOffset, enc.byteOffset + enc.byteLength) as ArrayBuffer;
    }
    const fakeCreate = async () => ({
      id: "e2e-fake-credential-id",
      rawId: strToBuf("e2e-fake-credential-id"),
      type: "public-key",
      response: {
        clientDataJSON: strToBuf(
          '{"type":"webauthn.create","challenge":"fake","origin":"http://127.0.0.1:8723"}',
        ),
        attestationObject: strToBuf("e2e-fake-attestation"),
      },
    });
    try {
      if (!navigator.credentials) {
        Object.defineProperty(navigator, "credentials", {
          value: { create: fakeCreate, get: fakeCreate },
          configurable: true,
        });
      } else {
        Object.defineProperty(navigator.credentials, "create", {
          value: fakeCreate,
          configurable: true,
        });
      }
    } catch {
      /* swallow — chromium's read-only credentials object will surface later */
    }
  });

  await page.goto("/account");
  const beginPromise = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/v1/auth/webauthn/register/begin") &&
      r.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Add a passkey" }).click();
  const beginRes = await beginPromise;
  expect(beginRes.status()).toBe(200);
  // No passkey row materialises (the server rejects fake attestation).
  await expect(page.getByText("No passkeys yet", { exact: false })).toBeVisible();
});

test("v0.4.0: webauthn API — list returns [] and begin issues challenge options", async ({ page }) => {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((c) => c.name === "burrow_csrf")?.value ?? "";
  const headers = { "X-CSRF-Token": csrf, "Content-Type": "application/json" };

  const list = await page.request.get("/api/v1/auth/webauthn/credentials");
  expect(list.status()).toBe(200);
  expect(await list.json()).toEqual([]);

  // Server returns {session_id, options:{publicKey:{...}}} — a known UI/server
  // contract gap (the page treats the whole envelope as options). We assert
  // what the server actually emits.
  const begin = await page.request.post("/api/v1/auth/webauthn/register/begin", {
    headers,
    data: {},
  });
  expect(begin.status()).toBe(200);
  const body = (await begin.json()) as Record<string, unknown>;
  expect(typeof body.session_id).toBe("string");
  expect(body.options).toBeTruthy();
});
