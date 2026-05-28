// test-only — never deploy this shape.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("16-webhooks: page renders + Add webhook flow opens", async ({ page }) => {
  await page.goto("/webhooks");
  await expect(page.getByRole("heading", { name: "Webhooks" })).toBeVisible();
  await expect(page.locator('table[aria-label="Webhooks"]')).toBeVisible();

  // Open Add webhook dialog (exercises the write path entrypoint).
  await page.getByRole("button", { name: "Add webhook" }).click();
  await expect(page.locator('[role="dialog"]')).toBeVisible({ timeout: 5_000 });
});
