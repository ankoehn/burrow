import { describe, it, expect, beforeEach } from "vitest";
import { resetDb } from "@/mocks/db";
import "@/mocks/server"; // installed via test setup; import asserts module loads

const CSRF = "test-csrf-token";
function authed(method: string): RequestInit {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (!["GET", "HEAD", "OPTIONS"].includes(method)) h["X-CSRF-Token"] = CSRF;
  return { method, headers: h, credentials: "include" };
}

describe("MSW handlers (contract fidelity)", () => {
  beforeEach(() => { resetDb(); document.cookie = `burrow_csrf=${CSRF}; path=/`; });

  it("GET /api/v1/me returns the seeded admin", async () => {
    const r = await fetch("/api/v1/me", authed("GET"));
    expect(r.status).toBe(200);
    const b = await r.json();
    expect(b).toEqual({ id: expect.any(String), email: "alice@acme.io", role: "admin" });
  });

  it("GET /api/v1/users is paginated and enveloped", async () => {
    const r = await fetch("/api/v1/users?limit=20&offset=0", authed("GET"));
    const b = await r.json();
    expect(Array.isArray(b.users)).toBe(true);
    expect(typeof b.total).toBe("number");
    expect(b.users[0]).toHaveProperty("status");
    expect(b.users[0]).toHaveProperty("last_login");
  });

  it("POST /api/v1/users without CSRF header is 403", async () => {
    const r = await fetch("/api/v1/users", {
      method: "POST", credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "x@y.io", password: "password123", role: "user" }),
    });
    expect(r.status).toBe(403);
    expect(await r.json()).toEqual({ error: "csrf token invalid" });
  });

  it("POST /api/v1/users duplicate email is 409", async () => {
    const r = await fetch("/api/v1/users", { ...authed("POST"), body: JSON.stringify({ email: "bob@acme.io", password: "password123", role: "user" }) });
    expect(r.status).toBe(409);
    expect(await r.json()).toEqual({ error: "email already in use" });
  });

  it("PATCH /api/v1/users/{me} status is 400 (self-status guard)", async () => {
    const me = await (await fetch("/api/v1/me", authed("GET"))).json();
    const r = await fetch(`/api/v1/users/${me.id}`, { ...authed("PATCH"), body: JSON.stringify({ status: "suspended" }) });
    expect(r.status).toBe(400);
    expect(await r.json()).toEqual({ error: "cannot change your own status" });
  });

  it("GET /api/v1/roles is a bare array; detail has permissions", async () => {
    const list = await (await fetch("/api/v1/roles", authed("GET"))).json();
    expect(Array.isArray(list)).toBe(true);
    const detail = await (await fetch("/api/v1/roles/user", authed("GET"))).json();
    expect(detail.permissions).toContain("tunnels:read:own");
  });

  it("GET /api/v1/settings never returns smtp.password", async () => {
    await fetch("/api/v1/settings", { ...authed("PUT"), body: JSON.stringify({ "smtp.host": "mx", "smtp.password": "leak" }) });
    const b = await (await fetch("/api/v1/settings", authed("GET"))).json();
    expect(b["smtp.host"]).toBe("mx");
    expect(b).not.toHaveProperty("smtp.password");
  });

  it("POST /api/v1/settings/test-email is 409 when unconfigured", async () => {
    const r = await fetch("/api/v1/settings/test-email", { ...authed("POST"), body: JSON.stringify({ to: "ops@acme.io" }) });
    expect(r.status).toBe(409);
  });

  it("GET /api/v1/clients/{id} flattens ClientView + services", async () => {
    const list = await (await fetch("/api/v1/clients", authed("GET"))).json();
    const id = list[0].session_id;
    const d = await (await fetch(`/api/v1/clients/${id}`, authed("GET"))).json();
    expect(d.session_id).toBe(id);
    expect(d).not.toHaveProperty("client");
    expect(d.services[0].access_mode).toBe("open");
  });

  it("PUT /api/v1/tunnels/{id}/access-mode rejects bad enum and accepts open", async () => {
    const bad = await fetch("/api/v1/tunnels/tnl_web01/access-mode", { ...authed("PUT"), body: JSON.stringify({ access_mode: "nope" }) });
    expect(bad.status).toBe(400);
    const ok = await fetch("/api/v1/tunnels/tnl_web01/access-mode", { ...authed("PUT"), body: JSON.stringify({ access_mode: "open" }) });
    expect(ok.status).toBe(204);
  });

  it("unauthenticated request is 401 (no session cookie scenario)", async () => {
    const r = await fetch("/api/v1/users", { method: "GET", headers: { "x-mock-unauth": "1" } });
    expect(r.status).toBe(401);
    expect(await r.json()).toEqual({ error: "unauthorized" });
  });
});
