// test-only — never deploy this shape.
//
// Spec 32 — Login rate-limit UI mirror.
// 11 bad creds from the UI → 11th attempt surfaces the rate-limit
// banner. The relay container sets BURROW_LOGIN_RATE_LIMIT_PER_IP=3
// (Dockerfile.relay), so the 4th attempt triggers a 429 and the UI
// shows the too-many-attempts banner well within the 90 s spec timeout.

import { test, expect } from "@playwright/test";

test("32-login-rate-limit: 11 bad creds show too-many-attempts banner", async ({ page }) => {
  test.slow(); // up to 11 attempts; rate-limit window is 60s
  await page.goto("/login");

  for (let i = 0; i < 11; i++) {
    await page.fill("#login-email", "nobody@x");
    await page.fill("#login-password", "wrong");
    await page.getByRole("button", { name: /^sign in$/i }).click();

    // After click, the form either shows "Invalid email or password" or
    // "too many login attempts". Check which we got before the next attempt.
    const banner = page.locator("[role='alert'], .signin-error");
    await expect(banner).toBeVisible({ timeout: 3_000 });
    const text = (await banner.innerText()).toLowerCase();
    if (text.includes("too many")) {
      // Got the rate-limit message — assertion succeeds.
      return;
    }
    if (i < 10) {
      // Clear the banner for the next iteration.
      await page.fill("#login-email", "");
      await page.fill("#login-password", "");
    }
  }
  // If we get here, the rate-limiter never tripped — fail with a clear message.
  throw new Error(
    "Login rate-limit banner never appeared after 11 attempts. " +
    "Verify BURROW_LOGIN_RATE_LIMIT_PER_IP override is plumbed through the harness, " +
    "or fall back to TestSec_LoginRateLimit as canonical coverage."
  );
});
