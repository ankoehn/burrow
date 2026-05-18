import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiFetch, ApiError } from "./api";

beforeEach(() => {
  vi.restoreAllMocks();
  // Reset document.cookie between tests.
  // In jsdom, individual cookie pairs can be cleared by setting them expired.
  if (typeof document !== "undefined") {
    document.cookie = "burrow_csrf=; Max-Age=-1; path=/";
  }
});

describe("apiFetch", () => {
  it("returns parsed JSON on 200", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: 1 }), { status: 200, headers: { "content-type": "application/json" } }) as any);
    expect(await apiFetch("/me")).toEqual({ ok: 1 });
  });
  it("throws ApiError with status on non-2xx", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "bad" }), { status: 400 }) as any);
    await expect(apiFetch("/x")).rejects.toMatchObject({ status: 400 } as ApiError);
  });
  it("sends credentials and /api/v1 prefix", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 200 }) as any);
    await apiFetch("/tunnels");
    expect(f).toHaveBeenCalledWith("/api/v1/tunnels", expect.objectContaining({ credentials: "include" }));
  });
  it("returns null for an empty 204 body", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }) as any);
    // Set the CSRF cookie so the POST is not blocked in the real server, but here
    // we only test the apiFetch client-side behaviour.
    if (typeof document !== "undefined") {
      document.cookie = "burrow_csrf=testtoken; path=/";
    }
    expect(await apiFetch("/auth/logout", { method: "POST" })).toBeNull();
  });
  it("throws ApiError(401) on 401 without mutating window.location", async () => {
    const initialHref = typeof globalThis.location !== "undefined" ? globalThis.location.href : undefined;
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 401 }) as any);
    await expect(apiFetch("/me")).rejects.toMatchObject({ status: 401 });
    // apiFetch must NOT do a full-page redirect — location must be unchanged
    if (typeof globalThis.location !== "undefined") {
      expect(globalThis.location.href).toBe(initialHref);
    }
  });

  describe("CSRF header", () => {
    it("sends X-CSRF-Token on POST when burrow_csrf cookie is present", async () => {
      if (typeof document !== "undefined") {
        document.cookie = "burrow_csrf=my-csrf-token; path=/";
      }
      const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 200 }) as any);
      await apiFetch("/tokens", { method: "POST" });
      const [, opts] = f.mock.calls[0] as [string, RequestInit];
      const headers = opts.headers as Record<string, string>;
      expect(headers["X-CSRF-Token"]).toBe("my-csrf-token");
    });

    it("sends X-CSRF-Token on DELETE when burrow_csrf cookie is present", async () => {
      if (typeof document !== "undefined") {
        document.cookie = "burrow_csrf=del-csrf-token; path=/";
      }
      const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }) as any);
      await apiFetch("/tokens/t1", { method: "DELETE" });
      const [, opts] = f.mock.calls[0] as [string, RequestInit];
      const headers = opts.headers as Record<string, string>;
      expect(headers["X-CSRF-Token"]).toBe("del-csrf-token");
    });

    it("does NOT send X-CSRF-Token on GET", async () => {
      if (typeof document !== "undefined") {
        document.cookie = "burrow_csrf=should-not-appear; path=/";
      }
      const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 200 }) as any);
      await apiFetch("/tunnels"); // default method is GET
      const [, opts] = f.mock.calls[0] as [string, RequestInit];
      const headers = opts.headers as Record<string, string>;
      expect(headers["X-CSRF-Token"]).toBeUndefined();
    });

    it("does NOT send X-CSRF-Token on HEAD", async () => {
      if (typeof document !== "undefined") {
        document.cookie = "burrow_csrf=should-not-appear; path=/";
      }
      const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 200 }) as any);
      await apiFetch("/me", { method: "HEAD" });
      const [, opts] = f.mock.calls[0] as [string, RequestInit];
      const headers = opts.headers as Record<string, string>;
      expect(headers["X-CSRF-Token"]).toBeUndefined();
    });

    it("sends no X-CSRF-Token header when cookie is absent (pre-login)", async () => {
      // Cookie cleared in beforeEach; POST /auth/login is exempt on the server.
      const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(
        new Response(JSON.stringify({ id: "u1" }), { status: 200 }) as any);
      await apiFetch("/auth/login", { method: "POST" });
      const [, opts] = f.mock.calls[0] as [string, RequestInit];
      const headers = opts.headers as Record<string, string>;
      // Header should be absent (or empty string filtered out); either way not
      // a truthy value — the server exempts login anyway.
      expect(headers["X-CSRF-Token"] || "").toBe("");
    });
  });
});
