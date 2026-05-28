// test-only — never deploy this shape.
//
// Custom domains UI surface check + (optional) end-to-end add flow.
// The full happy-path requires file uploads of the wildcard cert pair; the
// design system's file inputs are sometimes nested behind labels, making
// the upload path brittle. This spec verifies the panel renders for the
// admin and exits cleanly if the Custom domains tab isn't available.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("13-custom-domains: Custom domains tab + panel render", async ({ page, request }) => {
  const list = await request.get("/api/v1/services");
  const services = (await list.json()) as { id: string; name: string }[];
  const ai = services.find((s) => s.name === "ai");
  if (!ai) throw new Error("ai service not found");
  await page.goto(`/services/${ai.id}`);
  await expect(page.getByRole("heading", { name: /Service.*\bai\b/ })).toBeVisible();

  const tab = page.getByRole("tab", { name: /Custom domains/i });
  await expect(tab).toBeVisible();
  await tab.click();

  // The panel should render some interactive element (add button or input).
  const panelLoaded = await page
    .locator('[role="tabpanel"] :is(button, input)')
    .first()
    .isVisible({ timeout: 5_000 })
    .catch(() => false);
  expect(panelLoaded).toBe(true);
});
