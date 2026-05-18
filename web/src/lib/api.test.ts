import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiFetch, ApiError } from "./api";

beforeEach(() => { vi.restoreAllMocks(); (globalThis as any).location = { href: "/" }; });

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
    expect(await apiFetch("/auth/logout", { method: "POST" })).toBeNull();
  });
  it("redirects to /login and throws ApiError(401) on 401", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 401 }) as any);
    await expect(apiFetch("/me")).rejects.toMatchObject({ status: 401 });
    expect((globalThis as any).location.href).toBe("/login");
  });
});
