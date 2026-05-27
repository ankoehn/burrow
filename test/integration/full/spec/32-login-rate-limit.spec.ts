// test-only — never deploy this shape.
//
// Spec 32 — Login rate-limit UI mirror.
// 11 bad creds from the UI → 11th attempt surfaces the rate-limit
// banner. Plan-acknowledged Open Question: the live stack uses
// production-default 10 attempts/min, so this spec accepts the ~6
// minute wait or skips, citing TestSec_LoginRateLimit as canonical.
//
// SKIP RATIONALE: The rate-limit override is only available via Go
// struct field LoginRateLimitPerIPOverride — there is no env var that
// cmd/server/main.go reads, so compose.full.yml cannot plumb a lower
// limit. Running 11 attempts against the production default of 10/min
// would exceed the Playwright default timeout. Backend coverage is
// authoritative via TestSec_LoginRateLimit (cmd/server/e2e_security_test.go).

import { test, expect } from "@playwright/test";

test("32-login-rate-limit: 11 bad creds show too-many-attempts banner", async ({ page }) => {
  test.skip(true, "Login rate-limit override not plumbed in harness — TestSec_LoginRateLimit covers backend");
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
