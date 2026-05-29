// test-only — never deploy this shape.
//
// Covers the /connection-logs filter controls and the Export action that the
// feature suite did not yet exercise. See web/src/pages/ConnectionLogs.tsx:
//   - Kind / Service / Date-range filters are native <select> (aria-label=…).
//   - Rollups is a native checkbox (aria-label="Rollups").
//   - Export is a header button that issues GET /connection-logs/export
//     ?format=ndjson via fetch (apiFetch) — it does NOT trigger a browser
//     `download` event, so we assert via the network response (the brief's
//     documented fallback path). Read-only — no cleanup.
import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

// The page renders either a logs table, a rollups table (both
// aria-label="Connection logs"), or an EmptyState — depending on seeded data.
// "renders without error" == one of those is visible and no error banner shows.
async function assertViewSettled(page: import("@playwright/test").Page) {
  const table = page.locator('table[aria-label="Connection logs"]');
  const empty = page.getByText(/No connection logs yet|No rollups yet/);
  await expect.poll(
    async () => (await table.count()) > 0 || (await empty.count()) > 0,
    { timeout: 10_000, message: "neither logs table nor empty-state rendered" },
  ).toBe(true);
  // A thrown render error would surface React's fallback / a missing heading.
  // exact:true — the EmptyState renders an <h4>"No connection logs yet"</h4>
  // that substring-matches a loose "Connection logs".
  await expect(page.getByRole("heading", { name: "Connection logs", exact: true })).toBeVisible();
}

test("47-connection-logs-filter-export: rollups toggle + kind filter render; Export hits the API", async ({ page }) => {
  await page.goto("/connection-logs");
  await expect(page.getByRole("heading", { name: "Connection logs", exact: true })).toBeVisible();
  await assertViewSettled(page);

  // --- ROLLUPS TOGGLE ------------------------------------------------------
  // Native checkbox aria-label="Rollups". Toggle to rollup view, assert it
  // still renders, then toggle back to detail view.
  const rollups = page.getByRole("checkbox", { name: "Rollups", exact: true });
  await rollups.check();
  await expect(rollups).toBeChecked();
  await assertViewSettled(page);
  await rollups.uncheck();
  await expect(rollups).not.toBeChecked();
  await assertViewSettled(page);

  // --- KIND FILTER ---------------------------------------------------------
  // Native <select> aria-label="Kind". Pick "TCP proxy" and assert the view
  // re-settles without error (don't over-assert rows — seeded data varies).
  await page.getByLabel("Kind").selectOption("tcp_proxy");
  await assertViewSettled(page);
  // Reset to All.
  await page.getByLabel("Kind").selectOption("");
  await assertViewSettled(page);

  // --- EXPORT --------------------------------------------------------------
  // handleExport() issues GET /api/v1/connection-logs/export?format=ndjson via
  // fetch (no browser download event). Assert the network response is 200.
  const [resp] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes("/connection-logs/export") && r.request().method() === "GET",
      { timeout: 10_000 },
    ),
    page.getByRole("button", { name: /^export$/i }).click(),
  ]);
  expect(resp.status()).toBe(200);
  expect(resp.url()).toContain("format=ndjson");
});
