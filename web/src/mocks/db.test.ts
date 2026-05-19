import { describe, it, expect, beforeEach } from "vitest";
import { db, resetDb } from "@/mocks/db";

describe("mock db", () => {
  beforeEach(() => resetDb());

  it("seeds an admin (me) and a second user", () => {
    expect(db.me.role).toBe("admin");
    expect(db.users.length).toBeGreaterThanOrEqual(2);
    expect(db.users.find((u) => u.id === db.me.id)).toBeTruthy();
  });

  it("seeds two built-in roles with permissions", () => {
    const names = db.roles.map((r) => r.name).sort();
    expect(names).toEqual(["admin", "user"]);
    expect(db.rolePerms["admin"].length).toBeGreaterThan(0);
    expect(db.rolePerms["user"]).toContain("tunnels:read:own");
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
