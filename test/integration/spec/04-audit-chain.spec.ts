// test-only — never deploy this shape.

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("04-audit-chain: UI mint produces token.mint audit event; chain verifies", async ({ page }) => {
  // 1. Mint a token via /tokens so we have something to find in /audit.
  await page.goto("/tokens");
  const tokenName = `e2e-spec-4-${Date.now()}`;
  await page.fill("#token-name", tokenName);
  await page.getByRole("button", { name: "Create" }).click();
  const mintDialog = page.getByRole("dialog", { name: "Copy your token now" });
  await expect(mintDialog).toBeVisible({ timeout: 5_000 });
  await mintDialog.getByRole("button", { name: "Done" }).click();

  // 2. Open /audit and search for the token name.
  await page.goto("/audit");
  await expect(page.getByRole("heading", { name: "Audit log" })).toBeVisible();
  await page.getByRole("searchbox", { name: "Search audit events" }).fill(tokenName);

  // 3. The filtered table must contain a row whose Action cell is exactly "token.mint".
  // Action string is locked: internal/audit/actions.go ActionTokenMint = "token.mint".
  const auditTable = page.locator('table[aria-label="Audit events"]');
  await expect(auditTable).toBeVisible({ timeout: 5_000 });

  await expect
    .poll(async () => auditTable.locator("tbody tr").filter({ hasText: "token.mint" }).count(), {
      timeout: 5_000,
      message: "Expected at least one row with Action=token.mint after UI mint",
    })
    .toBeGreaterThanOrEqual(1);

  // 4. The same filtered table must contain a row whose Subject column has the token name.
  await expect(auditTable.locator("tbody tr").filter({ hasText: tokenName })).toHaveCount(1);

  // 5. Verify the chain (POST /audit/verify); expect a green notice.
  await page.getByRole("button", { name: "Verify chain" }).click();
  await expect(page.locator(".notice-inline.ok"))
    .toContainText(/Chain valid from/, { timeout: 5_000 });
});
