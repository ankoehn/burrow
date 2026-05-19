import type {
  UserAdmin, RoleSummary, Session, ClientDetail, SettingsMap,
  Service, ServiceApiKey,
} from "@/lib/contract";

// Internal service row: the wire Service plus the owning user_id (stripped
// before serialization — owner-scoping the v0.3.0 /services surface).
export interface ServiceRow extends Service {
  user_id: string;
}

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
  services: ServiceRow[];
  serviceApiKeys: Record<string, ServiceApiKey[]>;
  serviceAccessPolicy: Record<string, string[]>;
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
    services: [
      { id: "svc_web01", user_id: meId, name: "web", type: "http", subdomain: "k7p2qx", hostname: "k7p2qx.tunnels.example.com", access_mode: "open", api_key_header: "Authorization", connected: true, remote_port: 0, local_addr: "127.0.0.1:3000" },
      { id: "svc_ai001", user_id: meId, name: "ollama", type: "http", subdomain: "ai4m2q", hostname: "ai4m2q.tunnels.example.com", access_mode: "api_key", api_key_header: "Authorization", connected: true, remote_port: 0, local_addr: "127.0.0.1:11434" },
      { id: "svc_graf01", user_id: meId, name: "grafana", type: "http", subdomain: "gf7x1p", hostname: "gf7x1p.tunnels.example.com", access_mode: "burrow_login", api_key_header: "Authorization", connected: false, remote_port: 0, local_addr: "127.0.0.1:3001" },
      { id: "svc_pg001", user_id: meId, name: "postgres", type: "tcp", subdomain: "", hostname: "", access_mode: "open", api_key_header: "Authorization", connected: true, remote_port: 9000, local_addr: "127.0.0.1:5432" },
    ],
    serviceApiKeys: {
      svc_ai001: [
        { id: "sak_ci01", name: "ci", last_used: null, created_at: "2026-05-10T08:00:00Z" },
        { id: "sak_prod1", name: "prod", last_used: "2026-05-18T09:00:00Z", created_at: "2026-05-01T08:00:00Z" },
      ],
    },
    serviceAccessPolicy: {
      svc_graf01: ["user"],
    },
  };
}

export let db: MockDb = seed();

export function resetDb(): void {
  db = seed();
}
