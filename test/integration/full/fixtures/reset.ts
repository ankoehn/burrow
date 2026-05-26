// test-only — never deploy this shape.
// Hits the build-tagged /api/v1/internal/test-reset endpoint (Plan 1 T18).
// Truncates mutable tables (audit, tokens, sessions, services + per-service
// rows, webhooks, connection-logs, rate limits, model_aliases, budgets,
// automation tokens, non-seeded users) while preserving migrations + the
// seeded admin.

import type { APIRequestContext } from "@playwright/test";
import { RESET_URL } from "./env";

export async function resetMutableTables(request: APIRequestContext): Promise<void> {
  const res = await request.post(RESET_URL);
  if (res.status() !== 204) {
    throw new Error(`test-reset returned ${res.status()}; is relay built with -tags=integration?`);
  }
}
