import type {
  UserAdmin, RoleSummary, Session, ClientDetail, SettingsMap,
} from "@/lib/contract";

export interface MockDb {
  me: { id: string; email: string; role: "admin" | "user" };
  csrf: string;
  users: UserAdmin[];
  roles: RoleSummary[];
  rolePerms: Record<string, string[]>;
  sessions: Session[];
  settings: SettingsMap;
  smtpPasswordSet: boolean;
  clients: ClientDetail[];
  tokens: { id: string; name: string; last_used: string | null; created_at: string }[];
}

function seed(): MockDb {
  const meId = "bur_usr_admin01";
  return {
    me: { id: meId, email: "alice@acme.io", role: "admin" },
    csrf: "test-csrf-token",
    users: [
      { id: meId, email: "alice@acme.io", role: "admin", status: "active", last_login: "2026-05-18T09:00:00Z", created_at: "2026-01-12T08:00:00Z" },
      { id: "bur_usr_bob0002", email: "bob@acme.io", role: "user", status: "active", last_login: null, created_at: "2026-02-01T08:00:00Z" },
      { id: "bur_usr_carol03", email: "carol@acme.io", role: "user", status: "suspended", last_login: "2026-04-10T12:00:00Z", created_at: "2026-03-01T08:00:00Z" },
    ],
    roles: [
      { name: "admin", description: "Full access — manage tunnels, tokens, users and roles.", created_at: "2026-01-01T00:00:00Z" },
      { name: "user", description: "Use own tunnels and tokens; manage own account.", created_at: "2026-01-01T00:00:00Z" },
    ],
    rolePerms: {
      admin: ["tunnels:read:any", "tunnels:manage:any", "tokens:manage:any", "services:configure:any", "sessions:manage:any", "users:read", "users:manage", "roles:read", "settings:manage"],
      user: ["tunnels:read:own", "tunnels:manage:own", "tokens:manage:own", "services:configure:own", "sessions:manage:own"],
    },
    sessions: [
      { id: "sess_cur", ip: "203.0.113.7", user_agent: "Mozilla/5.0 (current)", created_at: "2026-05-18T09:00:00Z", expires_at: "2026-05-25T09:00:00Z", current: true },
      { id: "sess_old", ip: "198.51.100.4", user_agent: "Mozilla/5.0 (laptop)", created_at: "2026-05-10T09:00:00Z", expires_at: "2026-05-17T09:00:00Z", current: false },
    ],
    settings: {},
    smtpPasswordSet: false,
    clients: [
      {
        session_id: "sess_4f7a9c0b2e81", user_id: meId, token_name: "office-box-1",
        remote_addr: "203.0.113.7:51234", os: "linux", arch: "amd64", client_version: "0.2.0",
        service_count: 1, total_bytes_in: 10240, total_bytes_out: 4096,
        services: [
          { id: "tnl_web01", name: "web-staging", type: "tcp", remote_port: 9000, local_addr: "127.0.0.1:3000", access_mode: "open", bytes_in: 2048, bytes_out: 1024, total_bytes_in: 10240, total_bytes_out: 4096 },
        ],
      },
    ],
    tokens: [
      { id: "tok_1", name: "office-box-1", last_used: "2026-05-18T09:00:00Z", created_at: "2026-05-01T08:00:00Z" },
    ],
  };
}

export let db: MockDb = seed();

export function resetDb(): void {
  db = seed();
}
