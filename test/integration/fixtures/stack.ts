// test-only — never deploy this shape.
//
// Shell-out helpers around `docker compose`. Uses Node's stdlib
// child_process — no npm dep on a docker library.

import { execSync } from "node:child_process";
import path from "node:path";
import { COMPOSE_FILE } from "./env";

// All commands run from the repo root so the relative COMPOSE_FILE path
// resolves correctly regardless of where Playwright is invoked from.
function repoRoot(): string {
  // playwright.config.ts and this file live under test/integration/;
  // process.cwd() during a Playwright run is test/integration/.
  return path.resolve(process.cwd(), "..", "..");
}

function compose(args: string): void {
  execSync(`docker compose -f ${COMPOSE_FILE} ${args}`, {
    cwd: repoRoot(),
    stdio: "inherit",
  });
}

export function composeRestartRelay(): void {
  compose("restart relay");
}
