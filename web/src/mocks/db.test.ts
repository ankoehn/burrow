import { describe, it, expect, beforeEach } from "vitest";
import { db, resetDb } from "@/mocks/db";

describe("mock db", () => {
  beforeEach(() => resetDb());

  it("seeds an admin (me) and a second user", () => {
    expect(db.me.role).toBe("admin");
    expect(db.users.length).toBeGreaterThanOrEqual(2);
    expect(db.users.find((u) => u.id === db.me.id)).toBeTruthy();
  });

  it("seeds the two built-in roles with permissions (v0.4.0 adds a custom 'analyst' alongside)", () => {
    const names = db.roles.map((r) => r.name).sort();
    expect(names).toContain("admin");
    expect(names).toContain("user");
    expect(db.rolePerms["admin"].length).toBeGreaterThan(0);
    expect(db.rolePerms["user"]).toContain("tunnels:read:own");
    // v0.4.0 ships a custom role in the seed so the Roles page Edit flow has
    // a row to act on without needing to first create one.
    const analyst = db.roles.find((r) => r.name === "analyst");
    expect(analyst).toBeDefined();
    expect(analyst!.builtin).toBe(false);
  });

  it("seeds at least one session marked current", () => {
    expect(db.sessions.some((s) => s.current)).toBe(true);
  });

  it("seeds a client with a service in open mode", () => {
    expect(db.clients.length).toBeGreaterThan(0);
    const svc = db.clients[0].services[0];
    expect(svc.access_mode).toBe("open");
  });

  it("resetDb restores mutations", () => {
    db.users.pop();
    const shrunk = db.users.length;
    resetDb();
    expect(db.users.length).toBeGreaterThan(shrunk);
  });
});
