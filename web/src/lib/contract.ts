// Typed mirror of docs/superpowers/specs/2026-05-19-v0.2.0-api-contract.md.
// snake_case fields match the wire verbatim; do not rename.

export type UserRole = "admin" | "user";
export type UserStatus = "active" | "suspended";

export interface UserAdmin {
  id: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  last_login: string | null;
  created_at: string;
}

export interface UsersPage {
  users: UserAdmin[];
  total: number;
}

export interface RoleSummary {
  name: string;
  description: string;
  created_at: string;
}

export interface RoleDetail extends RoleSummary {
  permissions: string[];
}

export interface Session {
  id: string;
  ip: string;
  user_agent: string;
  created_at: string;
  expires_at: string;
  current: boolean;
}

export type SettingsMap = Record<string, string>;
export type SmtpTls = "none" | "starttls" | "implicit";

export const ACCESS_MODES = ["open", "api_key", "burrow_login"] as const;
export type AccessMode = (typeof ACCESS_MODES)[number];
export function isAccessMode(v: string): v is AccessMode {
  return (ACCESS_MODES as readonly string[]).includes(v);
}

export interface ServiceView {
  id: string;
  name: string;
  type: string;
  remote_port: number;
  local_addr: string;
  access_mode: AccessMode;
  bytes_in: number;
  bytes_out: number;
  total_bytes_in: number;
  total_bytes_out: number;
}

export interface ClientView {
  session_id: string;
  user_id: string;
  token_name: string;
  remote_addr: string;
  os: string;
  arch: string;
  client_version: string;
  service_count: number;
  total_bytes_in: number;
  total_bytes_out: number;
}

export interface ClientDetail extends ClientView {
  services: ServiceView[];
}

export interface NewToken {
  name: string;
  token: string;
}

// ---- v0.3.0: durable services + HTTP access config ----
// Mirror of docs/superpowers/specs/2026-05-19-v0.3.0-api-contract.md Part C/D/E.

export interface Service {
  id: string;
  name: string;
  type: "tcp" | "http";
  subdomain: string;
  hostname: string;
  access_mode: AccessMode;
  api_key_header: string;
  connected: boolean;
  remote_port: number;
  local_addr: string;
}

export interface ServiceDetail extends Service {
  api_key_count: number;
  access_policy: string[];
}

export interface ServiceApiKey {
  id: string;
  name: string;
  last_used: string | null;
  created_at: string;
}

export interface AccessPolicy {
  roles: string[];
}

// create-key response (plaintext, shown once)
export interface CreatedApiKey {
  id: string;
  name: string;
  key: string;
}
