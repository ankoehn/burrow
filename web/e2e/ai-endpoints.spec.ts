import { test, expect } from "@playwright/test";

// v0.4.0: AI endpoints dashboard surface. The AI GATEWAY nav group is gated
// on ≥1 service with access_mode="api_key" (Layout.tsx). The e2e fixture has
// no live services, so the nav links don't render — we navigate by URL.
//
// The backend route `/api/v1/ai/endpoints` is not wired by the relay binary;
// it exists in the MSW contract mock used by Vitest. The real server returns
// 404, so the page mounts in its error state — that's still a deterministic
// surface to assert: heading, metric tile labels, the "Retry" button.

// Use the globalSetup-cached admin session (see web/e2e/global-setup.ts).
test.use({ storageState: "playwright-auth.json" });

test("v0.4.0: AI endpoints page mounts with heading + metric strip", async ({ page }) => {
  // Direct URL nav — AI GATEWAY group only appears when an api_key service is
  // listed, which requires a live burrow client (none in the e2e fixture).
  await page.goto("/ai/endpoints");
  await expect(page).toHaveURL(/\/ai\/endpoints$/);
  await expect(page.getByRole("heading", { name: "AI endpoints" })).toBeVisible();
  await expect(
    page.getByText("OpenAI-compatible API", { exact: false }),
  ).toBeVisible();

  // Metric strip — four tiles. The cost tile reads /cost/summary, which DOES
  // exist server-side, so the strip mounts even with no live endpoints.
  const strip = page.getByRole("list", { name: "AI endpoint metrics" });
  await expect(strip).toBeVisible();
  await expect(strip.getByText("Requests (24h)", { exact: true })).toBeVisible();
  await expect(strip.getByText("Tokens in/out (24h)", { exact: true })).toBeVisible();
  await expect(strip.getByText("Cost estimate (24h)", { exact: true })).toBeVisible();
  await expect(strip.getByText("Cache hit ratio (24h)", { exact: true })).toBeVisible();

  // /ai/endpoints isn't wired in the real binary → page surfaces error state.
  await expect(
    page.getByText("Couldn't load AI endpoints", { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("v0.4.0: AI endpoints depends on /cost/summary contract", async ({ page }) => {
  // The page consumes /api/v1/cost/summary?window=today for its cost tile —
  // this route IS wired in the real server (router.go: GetCostSummary).
  const summary = await page.request.get("/api/v1/cost/summary?window=today");
  expect(summary.status()).toBe(200);
  const body = (await summary.json()) as Record<string, unknown>;
  expect(body).toMatchObject({ window: "today" });
  expect(typeof body.total_usd).toBe("number");
  expect(typeof body.tokens_in).toBe("number");
  expect(typeof body.tokens_out).toBe("number");
});
