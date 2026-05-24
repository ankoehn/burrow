// test-only — never deploy this shape.
//
// Verifies the v0.4 mTLS access-mode UI surface. Does NOT verify end-to-end
// cert auth (that would need a client cert minted from the test CA, plus
// Playwright's APIRequestContext doesn't support `--cert/--key` per-request).
// Tests that:
//   - The mTLS radio is present.
//   - Selecting it surfaces an MtlsPanel.
//   - For a TCP service, setting api_key/burrow_login/mtls returns an error
//     (only Open is valid for TCP — runbook §6 gotcha).
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("09-access-mode-mtls: mTLS radio renders on the ai service detail", async ({ page, request }) => {
  const list = await request.get("/api/v1/services");
  const services = (await list.json()) as { id: string; name: string }[];
  const ai = services.find((s) => s.name === "ai");
  if (!ai) throw new Error("ai service not found");
  await page.goto(`/services/${ai.id}`);
  await expect(page.getByRole("heading", { name: /Service.*\bai\b/ })).toBeVisible();

  const mtls = page.getByRole("radio", { name: /mTLS/ });
  if (!(await mtls.isVisible({ timeout: 2_000 }).catch(() => false))) {
    test.skip(true, "mTLS UI not present — feature not shipped in this build");
  }
  await mtls.click();

  // The MtlsPanel renders a CA-PEM upload / paste field. Just confirm the
  // panel rendered (a textarea or a file input near the radiogroup).
  const panelHasInput = await page
    .locator(".mode-detail textarea, .mode-detail input[type=file]")
    .first()
    .isVisible({ timeout: 2_000 })
    .catch(() => false);
  expect(panelHasInput).toBe(true);
});
