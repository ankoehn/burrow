// test-only — never deploy this shape.
//
// Spec 26 — MCP tool inventory.
// Primary: API coverage of /api/v1/mcp/tools (always asserted).
// Secondary: UI coverage if a /settings/mcp page or MCP card exists (optional,
//   no skip if absent — MCP page is not yet surfaced in the dashboard).

import { test, expect } from "@playwright/test";
import { AUTH_STORAGE_PATH } from "../../fixtures/auth";

test.use({ storageState: AUTH_STORAGE_PATH });

test("26-mcp: tool inventory — API returns ≥10 tools; UI surface optional", async ({ page, request }) => {
  // 1. API: backend must expose ≥10 MCP tools.
  const apiResp = await request.get("/api/v1/mcp/tools");
  expect(apiResp.status(), "GET /api/v1/mcp/tools").toBe(200);
  const tools = (await apiResp.json()) as unknown[];
  expect(Array.isArray(tools), "tools is array").toBe(true);
  expect(tools.length, `tool count (${tools.length}) ≥ 10`).toBeGreaterThanOrEqual(10);

  // 2. UI surface: optional — check /settings for an MCP card.
  //    If not found, the test passes on API coverage alone.
  await page.goto("/settings");
  const mcpCard = page.getByRole("link", { name: /MCP/i }).first();
  const hasCard = await mcpCard.isVisible({ timeout: 2_000 }).catch(() => false);
  if (!hasCard) {
    // No MCP UI surface in this build — API coverage above is the full test.
    return;
  }

  // MCP card found — click through and assert heading + tool listing.
  await mcpCard.click();
  const heading = page.getByRole("heading", { name: /MCP/i });
  await expect(heading).toBeVisible({ timeout: 5_000 });
  const toolItems = page.locator("li, tr").filter({ hasText: /^[a-z]+(\.[a-z_]+)+$/i });
  await expect(toolItems.first()).toBeVisible({ timeout: 5_000 });
});
