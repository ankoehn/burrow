import { describe, it, expect } from "vitest";
import type {
  UserAdmin, UsersPage, RoleSummary, RoleDetail, Session,
  ClientView, ClientDetail, ServiceView, SettingsMap, AccessMode,
} from "@/lib/contract";
import { ACCESS_MODES, isAccessMode } from "@/lib/contract";

describe("contract", () => {
  it("exposes the access-mode enum and guard", () => {
    expect(ACCESS_MODES).toEqual(["open", "api_key", "burrow_login"]);
    expect(isAccessMode("open")).toBe(true);
    expect(isAccessMode("nope")).toBe(false);
  });

  it("types compile against representative wire objects", () => {
    const u: UserAdmin = { id: "u1", email: "a@b.io", role: "admin", status: "active", last_login: null, created_at: "2026-01-01T00:00:00Z" };
    const page: UsersPage = { users: [u], total: 1 };
    const rs: RoleSummary = { name: "admin", description: "", created_at: "2026-01-01T00:00:00Z" };
    const rd: RoleDetail = { ...rs, permissions: ["tunnels:read:any"] };
    const s: Session = { id: "s1", ip: "1.2.3.4", user_agent: "UA", created_at: "x", expires_at: "y", current: true };
    const sv: ServiceView = { id: "t1", name: "web", type: "tcp", remote_port: 9000, local_addr: "127.0.0.1:3000", access_mode: "open", bytes_in: 0, bytes_out: 0, total_bytes_in: 0, total_bytes_out: 0 };
    const cv: ClientView = { session_id: "sess1", user_id: "u1", token_name: "tok", remote_addr: "1.2.3.4:5", os: "linux", arch: "amd64", client_version: "0.2.0", service_count: 1, total_bytes_in: 0, total_bytes_out: 0 };
    const cd: ClientDetail = { ...cv, services: [sv] };
    const sm: SettingsMap = { "smtp.host": "mx" };
    const am: AccessMode = "open";
    expect([u, page, rs, rd, s, cv, cd, sm, am]).toHaveLength(9);
  });
});
