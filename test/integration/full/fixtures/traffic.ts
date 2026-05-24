// test-only — never deploy this shape.
// Helpers for driving curl-like traffic through the tunnels from within a spec.

import type { APIRequestContext } from "@playwright/test";
import {
  HTTPS_INGRESS,
  TUNNEL_TCP_URL,
  TUNNEL_MULTI_A,
  TUNNEL_MULTI_B,
  aiHost,
} from "./env";

// Hits the TCP echo tunnel on its --remote port (9002). Each call is a fresh
// HTTP request; combined with curl-style behavior these become distinct
// sessions in the connection-logs ledger.
export async function pingTcpTunnel(request: APIRequestContext, n = 5): Promise<void> {
  for (let i = 0; i < n; i++) {
    const res = await request.post(`${TUNNEL_TCP_URL}/echo`, { data: { i } });
    if (res.status() !== 200) throw new Error(`tcp ping ${i} status ${res.status()}`);
  }
}

// Hits both client-multi services in turn (svc-a + svc-b).
export async function pingMultiTunnels(request: APIRequestContext): Promise<void> {
  for (const url of [TUNNEL_MULTI_A, TUNNEL_MULTI_B]) {
    const res = await request.get(`${url}/healthz`);
    if (res.status() !== 200) throw new Error(`multi ping ${url} status ${res.status()}`);
  }
}

// Hits the AI tunnel through the HTTPS proxy on :8443. The route is host-
// routed: we set the Host header to <subdomain>.test.local and the proxy
// forwards to the registered http tunnel (then to mockoai upstream).
//
// Returns the raw SSE body text. Throws on non-200.
export async function chatCompletions(
  request: APIRequestContext,
  apiKey: string | null,
  content: string,
  stream = true,
): Promise<{ status: number; body: string; headers: Record<string, string> }> {
  const headers: Record<string, string> = {
    "content-type": "application/json",
    host: aiHost(),
  };
  if (apiKey) headers["authorization"] = `Bearer ${apiKey}`;
  const res = await request.post(`${HTTPS_INGRESS}/v1/chat/completions`, {
    headers,
    data: { model: "mock", stream, messages: [{ role: "user", content }] },
    ignoreHTTPSErrors: true,
  });
  return { status: res.status(), body: await res.text(), headers: res.headers() };
}
