// test-only — never deploy this shape.
// Shell-out helpers around docker compose. Node stdlib only — no npm dep.

import { execSync } from "node:child_process";
import path from "node:path";
import { COMPOSE_FILE, COMPOSE_POSTGRES_OVERRIDE } from "./env";

function repoRoot(): string {
  return path.resolve(process.cwd(), "..", "..");
}

function compose(args: string): void {
  execSync(`docker compose -f ${COMPOSE_FILE} ${args}`, {
    cwd: repoRoot(),
    stdio: "inherit",
  });
}

function composePostgres(args: string): void {
  execSync(`docker compose -f ${COMPOSE_FILE} -f ${COMPOSE_POSTGRES_OVERRIDE} ${args}`, {
    cwd: repoRoot(),
    stdio: "inherit",
  });
}

export function composeUp(): void { compose("up -d --build --wait"); }
export function composeDown(): void { compose("down --volumes"); }
export function composeRestartRelay(): void { compose("restart relay"); }

export function composePostgresUp(): void { composePostgres("up -d --build --wait"); }
export function composePostgresDown(): void { composePostgres("down --volumes"); }
