#!/usr/bin/env node
/**
 * run-server.mjs — builds and starts burrowd for the Playwright e2e suite.
 * Works on both Windows and Linux (invoked by playwright.config.ts webServer).
 *
 * Required env vars (injected by Playwright's webServer.env):
 *   BURROW_ADMIN_EMAIL, BURROW_ADMIN_PASSWORD,
 *   BURROW_HTTP_LISTEN, BURROW_DATABASE_PATH, E2E_TMPDIR
 */
import { execSync, spawn } from "node:child_process";
import * as path from "node:path";
import * as fs from "node:fs";
import * as os from "node:os";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "../..");

const tmpDir = process.env.E2E_TMPDIR ?? fs.mkdtempSync(path.join(os.tmpdir(), "burrow-e2e-"));
fs.mkdirSync(tmpDir, { recursive: true });

const binaryName = process.platform === "win32" ? "burrowd-e2e.exe" : "burrowd-e2e";
const binary = path.join(tmpDir, binaryName);

console.log(`[run-server] building burrowd → ${binary}`);
execSync(`go build -o "${binary}" ./cmd/server`, {
  cwd: repoRoot,
  stdio: "inherit",
  env: { ...process.env },
});

console.log(`[run-server] starting burrowd serve --dev-certs (cwd=${tmpDir})`);
const child = spawn(binary, ["serve", "--dev-certs"], {
  cwd: tmpDir,
  stdio: "inherit",
  env: { ...process.env },
});

child.on("error", (err) => {
  console.error("[run-server] spawn error:", err);
  process.exit(1);
});

child.on("exit", (code) => {
  if (code !== 0 && code !== null) {
    console.error(`[run-server] burrowd exited with code ${code}`);
    process.exit(code ?? 1);
  }
});

// Forward SIGINT/SIGTERM so Playwright can stop the server cleanly.
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, () => {
    child.kill();
  });
}
