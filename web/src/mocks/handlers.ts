import { http, HttpResponse } from "msw";
import { db, type MockDb } from "@/mocks/db";
import type { AiEndpoint, CostSummary } from "@/lib/contract";

const json = (body: unknown, status = 200) => HttpResponse.json(body as object, { status });
const err = (status: number, message: string) => HttpResponse.json({ error: message }, { status });
const noContent = () => new HttpResponse(null, { status: 204 });

const SAFE = new Set(["GET", "HEAD", "OPTIONS"]);
const WHITELIST = ["smtp.host", "smtp.port", "smtp.username", "smtp.from", "smtp.tls"];

// Gate: replicate 401 -> 403(csrf) -> 403(admin) ordering.
function gate(req: Request, opts: { admin?: boolean } = {}): Response | null {
  if (req.headers.get("x-mock-unauth") === "1") return err(401, "unauthorized");
  const method = req.method.toUpperCase();
  if (!SAFE.has(method)) {
    if (req.headers.get("X-CSRF-Token") !== db.csrf) return err(403, "csrf token invalid");
  }
  if (opts.admin && db.me.role !== "admin") return err(403, "admin required");
  return null;
}

// services:configure — admin holds :any; the owner holds :own (spec Part C).
function canConfigure(svc: { user_id: string }): boolean {
  if (db.me.role === "admin") return true;
  return svc.user_id === db.me.id;
}

// Parse via text()+JSON.parse rather than req.json(): under jsdom/undici the
// Request#json() stream read is intermittently flaky, whereas text() is stable.
async function body<T>(req: Request): Promise<T | null> {
  try {
    const t = await req.text();
    if (!t) return null;
    return JSON.parse(t) as T;
  } catch { return null; }
}

export const handlers = [
  // ---- auth / identity ----
  http.get("/api/v1/me", ({ request }) => gate(request) ?? json(db.me)),
  http.post("/api/v1/auth/logout", ({ request }) => gate(request) ?? noContent()),
  http.post("/api/v1/auth/change-password", async ({ request }) => {
    const g = gate(request); if (g) return g;
    const b = await body<{ current_password?: string; new_password?: string }>(request);
    if (!b?.current_password || !b?.new_password) return err(400, "current_password and new_password are required");
    if (b.current_password !== "password123") return err(401, "current password is incorrect");
    if (b.new_password.length < 8) return err(400, "new password must be at least 8 characters");
    return noContent();
  }),

  // ---- users ----
  http.get("/api/v1/users", ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const url = new URL(request.url);
    const q = (url.searchParams.get("q") ?? "").toLowerCase();
    const limit = Number(url.searchParams.get("limit")) || 50;
    const offset = Number(url.searchParams.get("offset")) || 0;
    const filtered = db.users.filter((u) => u.email.toLowerCase().includes(q));
    return json({ users: filtered.slice(offset, offset + limit), total: filtered.length });
  }),
  http.post("/api/v1/users", async ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const b = await body<{ email?: string; password?: string; role?: string }>(request);
    if (!b?.email || !b?.password || !b?.role) return err(400, "email, password, and role are required");
    if (db.users.some((u) => u.email === b.email)) return err(409, "email already in use");
    if (b.password.length < 8) return err(400, "password must be at least 8 characters");
    if (b.role !== "admin" && b.role !== "user") return err(400, "role must be 'admin' or 'user'");
    const u: MockDb["users"][number] = {
      id: `bur_usr_${Math.random().toString(36).slice(2, 9)}`,
      email: b.email, role: b.role, status: "active", last_login: null,
      created_at: new Date().toISOString(),
    };
    db.users.push(u);
    return json(u, 201);
  }),
  http.patch("/api/v1/users/:id", async ({ request, params }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const b = await body<{ role?: string; status?: string }>(request);
    if (!b || (b.role == null && b.status == null)) return err(400, "role and/or status required");
    const u = db.users.find((x) => x.id === params.id);
    if (!u) return err(404, "user not found");
    if (b.status != null) {
      if (b.status !== "active" && b.status !== "suspended") return err(400, "status must be 'active' or 'suspended'");
      if (u.id === db.me.id) return err(400, "cannot change your own status");
      u.status = b.status;
    }
    if (b.role != null) {
      if (b.role !== "admin" && b.role !== "user") return err(400, "role must be 'admin' or 'user'");
      u.role = b.role;
    }
    return noContent();
  }),
  http.delete("/api/v1/users/:id", ({ request, params }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    if (params.id === db.me.id) return err(400, "cannot delete yourself");
    const i = db.users.findIndex((x) => x.id === params.id);
    if (i < 0) return err(404, "user not found");
    db.users.splice(i, 1);
    return noContent();
  }),

  // ---- roles ----
  http.get("/api/v1/roles", ({ request }) => gate(request, { admin: true }) ?? json(db.roles)),
  http.get("/api/v1/roles/:name", ({ request, params }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const r = db.roles.find((x) => x.name === params.name);
    if (!r) return err(404, "role not found");
    return json({ ...r, permissions: db.rolePerms[r.name] ?? [] });
  }),

  // ---- sessions ----
  http.get("/api/v1/sessions", ({ request }) => gate(request) ?? json(db.sessions)),
  http.delete("/api/v1/sessions/:id", ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const i = db.sessions.findIndex((s) => s.id === params.id);
    if (i < 0) return err(404, "session not found");
    db.sessions.splice(i, 1);
    return noContent();
  }),
  http.post("/api/v1/sessions/revoke-all", ({ request }) => {
    const g = gate(request); if (g) return g;
    const before = db.sessions.length;
    db.sessions = db.sessions.filter((s) => s.current);
    return json({ revoked: before - db.sessions.length });
  }),

  // ---- settings ----
  http.get("/api/v1/settings", ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const out: Record<string, string> = {};
    for (const k of WHITELIST) if (db.settings[k] != null) out[k] = db.settings[k];
    return json(out);
  }),
  http.put("/api/v1/settings", async ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const b = await body<Record<string, string>>(request);
    if (!b) return err(400, "invalid request body");
    if (b["smtp.tls"] != null && !["none", "starttls", "implicit"].includes(b["smtp.tls"]))
      return err(400, "smtp.tls must be none, starttls, or implicit");
    for (const k of WHITELIST) if (b[k] != null) db.settings[k] = b[k];
    return noContent();
  }),
  http.post("/api/v1/settings/test-email", async ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const b = await body<{ to?: string }>(request);
    if (!b?.to) return err(400, "to is required");
    if (!db.settings["smtp.host"] || !db.smtpPasswordSet)
      return err(409, "smtp is not configured — set host/port and BURROW_SMTP_PASSWORD");
    return noContent();
  }),

  // ---- clients ----
  http.get("/api/v1/clients", ({ request }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    return json(db.clients.map(({ services: _services, ...v }) => v));
  }),
  http.get("/api/v1/clients/:id", ({ request, params }) => {
    const g = gate(request, { admin: true }); if (g) return g;
    const c = db.clients.find((x) => x.session_id === params.id);
    if (!c) return err(404, "client not found");
    return json(c);
  }),

  // ---- per-service access mode ----
  http.put("/api/v1/tunnels/:id/access-mode", async ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const b = await body<{ access_mode?: string }>(request);
    if (!b?.access_mode) return err(400, "access_mode is required");
    let svc: MockDb["clients"][number]["services"][number] | undefined;
    for (const c of db.clients) { const s = c.services.find((x) => x.id === params.id); if (s) { svc = s; break; } }
    if (!svc) return err(404, "tunnel not found");
    if (!["open", "api_key", "burrow_login"].includes(b.access_mode))
      return err(400, "access_mode must be 'open', 'api_key', or 'burrow_login'");
    svc.access_mode = b.access_mode as typeof svc.access_mode;
    return noContent();
  }),

  // ---- v0.3.0 durable services (spec Part E) ----
  http.get("/api/v1/services", ({ request }) => {
    const g = gate(request); if (g) return g;
    // Owner-scoped; admin (tunnels:read:any) sees all.
    const rows = db.me.role === "admin"
      ? db.services
      : db.services.filter((s) => s.user_id === db.me.id);
    return json(rows.map(({ user_id: _u, ...s }) => s));
  }),
  http.get("/api/v1/services/:id", ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    const { user_id: _u, ...wire } = svc;
    return json({
      ...wire,
      api_key_count: (db.serviceApiKeys[svc.id] ?? []).length,
      access_policy: db.serviceAccessPolicy[svc.id] ?? [],
    });
  }),

  // ---- v0.3.0 per-service API keys (spec Part C; services:configure) ----
  http.get("/api/v1/services/:id/api-keys", ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    return json(db.serviceApiKeys[svc.id] ?? []);
  }),
  http.post("/api/v1/services/:id/api-keys", async ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    const b = await body<{ name?: string }>(request);
    if (!b?.name) return err(400, "name is required");
    const id = `sak_${Math.random().toString(36).slice(2, 8)}`;
    (db.serviceApiKeys[svc.id] ||= []).push({ id, name: b.name, last_used: null, created_at: new Date().toISOString() });
    return json({ id, name: b.name, key: `buk_mock_${Math.random().toString(36).slice(2, 18)}` }, 201);
  }),
  http.delete("/api/v1/services/:id/api-keys/:keyId", ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    const list = db.serviceApiKeys[svc.id] ?? [];
    const i = list.findIndex((k) => k.id === params.keyId);
    if (i < 0) return err(404, "api key not found");
    list.splice(i, 1);
    return noContent();
  }),

  // ---- v0.3.0 per-service access mode (canonical, service-scoped) ----
  http.put("/api/v1/services/:id/access-mode", async ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    const b = await body<{ access_mode?: string; api_key_header?: string }>(request);
    if (!b?.access_mode) return err(400, "access_mode is required");
    if (!["open", "api_key", "burrow_login"].includes(b.access_mode))
      return err(400, "access_mode must be 'open', 'api_key', or 'burrow_login'");
    if (b.access_mode !== "open" && svc.type === "tcp")
      return err(409, "api_key and burrow_login require an http service");
    svc.access_mode = b.access_mode as typeof svc.access_mode;
    if (b.access_mode === "api_key" && b.api_key_header) svc.api_key_header = b.api_key_header;
    return noContent();
  }),

  // ---- v0.3.0 per-service access policy (spec Part D; services:configure) ----
  http.get("/api/v1/services/:id/access-policy", ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    return json({ roles: db.serviceAccessPolicy[svc.id] ?? [] });
  }),
  http.put("/api/v1/services/:id/access-policy", async ({ request, params }) => {
    const g = gate(request); if (g) return g;
    const svc = db.services.find((s) => s.id === params.id);
    if (!svc) return err(404, "service not found");
    if (!canConfigure(svc)) return err(403, "forbidden");
    const b = await body<{ roles?: string[] }>(request);
    if (!b || !Array.isArray(b.roles)) return err(400, "roles is required");
    const known = new Set(db.roles.map((r) => r.name));
    for (const role of b.roles) if (!known.has(role)) return err(400, `unknown role "${role}"`);
    db.serviceAccessPolicy[svc.id] = [...b.roles];
    return noContent();
  }),

  // ---- tokens (Connect-a-client) ----
  http.get("/api/v1/tokens", ({ request }) => gate(request) ?? json(db.tokens)),
  http.post("/api/v1/tokens", async ({ request }) => {
    const g = gate(request); if (g) return g;
    const b = await body<{ name?: string }>(request);
    if (!b?.name) return err(400, "name is required");
    db.tokens.push({ id: `tok_${Math.random().toString(36).slice(2, 7)}`, name: b.name, last_used: null, created_at: new Date().toISOString() });
    return json({ name: b.name, token: `bur_${Math.random().toString(36).slice(2, 18)}` }, 201);
  }),

  // ---- events (inert SSE so Clients/Connect don't error) ----
  http.get("/api/v1/events", ({ request }) =>
    gate(request) ?? new HttpResponse("retry: 10000\n\n", { status: 200, headers: { "Content-Type": "text/event-stream" } })),

  // ---- tunnels (reference) ----
  http.get("/api/v1/tunnels", ({ request }) => gate(request) ?? json([])),

  // ---- v0.4.0 AI endpoints lens (spec §4.19) ----
  // Derived view over db.services where access_mode=api_key. Backend owns the
  // real metrics; the mock joins seeded aiMeta + modelAliases + serviceApiKeys.
  http.get("/api/v1/ai/endpoints", ({ request }) => {
    const g = gate(request); if (g) return g;
    const rows = db.me.role === "admin"
      ? db.services
      : db.services.filter((s) => s.user_id === db.me.id);
    const endpoints: AiEndpoint[] = rows
      .filter((s) => s.access_mode === "api_key")
      .map((s) => {
        const meta = db.aiMeta[s.id];
        const alias = db.modelAliases.find((a) => a.service_id === s.id);
        return {
          service_id: s.id,
          name: s.name,
          model_alias: alias?.alias ?? "",
          concrete_model: alias?.concrete_model ?? "",
          backend_type: meta?.backend_type ?? "other",
          api_key_count: (db.serviceApiKeys[s.id] ?? []).length,
          requests_24h: meta?.requests_24h ?? 0,
          cache_hits_24h: meta?.cache_hits_24h ?? 0,
          latency_p95_ms: meta?.latency_p95_ms ?? 0,
          status: meta?.status ?? "Offline",
          client_session_id: meta?.client_session_id ?? "",
        };
      });
    return json(endpoints);
  }),

  // ---- v0.4.0 cost summary (spec Part F) ----
  http.get("/api/v1/cost/summary", ({ request }) => {
    const g = gate(request); if (g) return g;
    const url = new URL(request.url);
    const w = (url.searchParams.get("window") ?? "today") as CostSummary["window"];
    const summary = db.costSummary[w] ?? db.costSummary.today;
    return json(summary);
  }),
];
