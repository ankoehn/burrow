// test-only — never deploy this shape.

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("03-token-mint: UI form mints a token; dialog reveals bur_*; list updates", async ({ page }) => {
  await page.goto("/tokens");
  await expect(page.getByRole("heading", { name: "Client tokens" })).toBeVisible();

  const tokenName = `e2e-spec-3-${Date.now()}`;

  await page.fill("#token-name", tokenName);
  await page.getByRole("button", { name: "Create" }).click();

  // Reveal dialog appears with the plaintext bur_*.
  const dialog = page.getByRole("dialog", { name: "Copy your token now" });
  await expect(dialog).toBeVisible({ timeout: 5_000 });

  const revealed = dialog.locator(".reveal-once .key-row .v");
  await expect(revealed).toBeVisible();
  const tokenText = (await revealed.innerText()).trim();
  expect(tokenText).toMatch(/^bur_/);
  expect(tokenText.length).toBeGreaterThanOrEqual(20);

  // Close dialog.
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(dialog).toBeHidden();

  // The new token now appears in the list.
  const row = page
    .locator('table[aria-label="Tokens"] tr')
    .filter({ hasText: tokenName });
  await expect(row).toBeVisible({ timeout: 5_000 });
});
