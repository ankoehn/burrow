// test-only — never deploy this shape.
// MUST match test/integration/full/compose.full.yml. Change one, change both.

import { spawnSync } from "node:child_process";

export const ADMIN_EMAIL = "admin@e2e.local";
export const ADMIN_PASSWORD = "e2e-pass";

export const DASHBOARD_URL = "http://localhost:8080";

// HTTPS proxy listener (wildcard cert). HTTP tunnels are HOST-ROUTED here — they
// have NO fixed --remote port. To target a specific http tunnel, send a request
// to HTTPS_INGRESS with the host header set to "<subdomain>.test.local"; the
// relay's proxy uses the host header to route to the registered tunnel.
//
// Plan-fidelity note: the plan-as-written assumed a fixed `TUNNEL_AI_URL =
// http://localhost:9001`. That binding does not exist — http-type tunnels
// only bind via the host-routed proxy on :8443.
export const HTTPS_INGRESS = "https://localhost:8443";

// TCP tunnels DO bind a fixed --remote port.
export const TUNNEL_TCP_URL = "http://localhost:9002";   // client-tcp echo
export const TUNNEL_MULTI_A = "http://localhost:9003";   // client-multi svc-a
export const TUNNEL_MULTI_B = "http://localhost:9004";   // client-multi svc-b

export const TUNNEL_NAMES = ["ai", "tcp-echo", "svc-a", "svc-b"] as const;

export const COMPOSE_FILE = "test/integration/full/compose.full.yml";
export const COMPOSE_POSTGRES_OVERRIDE = "test/integration/full/compose.full.postgres.yml";

export const RESET_URL = `${DASHBOARD_URL}/api/v1/internal/test-reset`;

// Discovers the AI tunnel's auto-assigned subdomain by tailing the relay log.
// HTTP tunnels register a random subdomain at session start. Cached after first
// call — call resetAiSubdomainCache() when the tunnel is known to have re-registered
// (e.g. after composeRestartRelay()).
let _aiSubdomain: string | undefined;
export function aiSubdomain(): string {
  if (_aiSubdomain) return _aiSubdomain;
  // Use spawnSync to avoid shell-pipe issues on Windows (cmd.exe lacks grep/tail).
  // Pull docker logs and filter in Node rather than relying on POSIX shell tools.
  const logs = spawnSync(
    "docker",
    ["logs", "burrow-e2e-full-relay-1"],
    { encoding: "utf8" },
  );
  const combined = (logs.stdout ?? "") + (logs.stderr ?? "");
  const lines = combined.split("\n").filter((l) => l.includes("http tunnel registered"));
  const out = lines[lines.length - 1] ?? "";
  const m = out.match(/subdomain=([a-z0-9]+)/);
  if (!m) throw new Error(`aiSubdomain: relay log has no "http tunnel registered" line yet`);
  _aiSubdomain = m[1];
  return _aiSubdomain;
}
export function resetAiSubdomainCache(): void {
  _aiSubdomain = undefined;
}
export function aiHost(): string {
  return `${aiSubdomain()}.test.local`;
}
