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
  // v0.4.0 additive — built-in roles are locked; custom roles are editable.
  // Older backends omit it; treat undefined as built-in for backwards safety.
  builtin?: boolean;
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

export const ACCESS_MODES = ["open", "api_key", "burrow_login", "mtls"] as const;
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

// ---- v0.4.0: AI gateway + company-scale dashboard ----
// Mirror of docs/superpowers/specs/2026-05-19-v0.4.0-api-contract.md.

// AI endpoint — read-only lens over Service rows where access_mode=api_key
// (spec §4.19). Backend derives; UI never POSTs this shape.
export interface AiEndpoint {
  service_id: string;
  name: string;
  model_alias: string;
  concrete_model: string;
  backend_type: "ollama" | "vllm" | "openai-compat" | "other";
  api_key_count: number;
  requests_24h: number;
  cache_hits_24h: number;
  latency_p95_ms: number;
  status: "Connected" | "Degraded" | "Offline";
  client_session_id: string;
}

// Service AI config (spec Part B.7) — one row per service, default-filled.
export interface CacheSettings {
  enabled: boolean;
  applies_per: "global" | "per_endpoint" | "per_api_key";
  ttl_seconds: number;
  max_entries: number;
  max_per_entry_kb: number;
}
export interface RedactionSettings {
  enabled: boolean;
  redact_for_logs_only: boolean;
  rule_ids: string[];
  presidio_enabled: boolean;
  presidio_url?: string;
}
export interface GuardrailSettings {
  enabled: boolean;
  action: "log_only" | "refuse_403" | "refuse_safe";
}
export interface InspectorSettings {
  enabled: boolean;
  max_requests: number;
}
export interface RoutingPolicy {
  strategy: "single" | "failover" | "weighted" | "header_based" | "sticky";
  model_alias: string;
  header_name: string;
  paused: boolean;
  circuit_breaker: { failure_pct: number; window_seconds: number; cool_down_seconds: number };
  backends: { service_id: string; weight: number; concrete_model: string }[];
  translate_to: "none" | "openai" | "anthropic";
}
export interface IpGeoConfig {
  enabled: boolean;
  allow_cidrs: string[];
  block_cidrs: string[];
  allow_countries: string[];
  block_countries: string[];
}
export interface MtlsConfig {
  enabled: boolean;
  ca_fingerprint_sha256: string;
  ca_pem?: string;
}
export interface ServiceAIConfig {
  cache: CacheSettings;
  redaction: RedactionSettings;
  guardrails: GuardrailSettings;
  inspector: InspectorSettings;
  routing: RoutingPolicy;
  ip_geo: IpGeoConfig;
  mtls: MtlsConfig;
}

// Usage + cost (spec Part F).
export interface UsageEvent {
  id: string;
  service_id: string;
  api_key_id: string;
  ts: string;
  kind: "openai" | "anthropic" | "mcp" | "unknown";
  tokens_in: number;
  tokens_out: number;
  bytes_in: number;
  bytes_out: number;
  streamed: boolean;
  cache_hit: boolean;
  upstream_status: number;
}
export interface PricingEntry {
  provider: string;
  model: string;
  input_per_million: number;
  output_per_million: number;
}
export interface PricingTable {
  version: string;
  entries: PricingEntry[];
}
export interface CostSummary {
  window: "today" | "week" | "month" | "year";
  total_usd: number;
  tokens_in: number;
  tokens_out: number;
  top_consumers: { api_key_id: string; service_id: string; tokens_in: number; tokens_out: number; usd: number }[];
  pct_of_budget: number | null;
}
export interface Budget {
  id: string;
  scope: "api_key" | "service" | "user" | "global";
  subject_id: string;
  daily_usd: number;
  action_on_exceed: "alert_webhook" | "throttle_zero" | "disable_key";
  alert_webhook_id: string | null;
  current_usd: number;
  exceeded: boolean;
}

// Rate limits / quotas (spec Part D).
export interface RateLimit {
  id: string;
  scope: "api_key" | "role" | "service" | "global";
  subject: string;
  dimension: "rpm" | "bpm";
  limit: number;
  burst: number;
  window?: "minute" | "day";
  created_at: string;
}

// Request inspector (spec Part E).
export interface InspectorEntry {
  id: string;
  service_id: string;
  api_key_id: string;
  ts: string;
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  bytes_in: number;
  bytes_out: number;
  req_headers: Record<string, string>;
  req_body: string;
  resp_headers: Record<string, string>;
  resp_body: string;
  truncated: boolean;
  cache: "HIT" | "MISS" | "SKIP";
  redactions: { rule: string; count: number }[];
  trace_id: string;
  remote_ip: string;
  mcp?: { method: string; tool: string; params: unknown };
}

// Audit (spec Part G).
export interface AuditEvent {
  id: string;
  ts: string;
  actor_id: string;
  actor_email: string;
  action: string;
  subject_id: string;
  subject_label: string;
  result: "ok" | "denied" | "error";
  source_ip: string;
  user_agent: string;
  request_id: string;
  payload: Record<string, unknown>;
  prev_hash: string;
  hash: string;
}
export interface AuditFingerprint {
  public_key: string;
  fingerprint: string;
}

// Webhooks (spec Part H).
export interface Webhook {
  id: string;
  name: string;
  url: string;
  events: string[];
  paused: boolean;
  consecutive_failures: number;
  first_failure_at: string | null;
  created_at: string;
}
export interface CreatedWebhook {
  webhook: Webhook;
  signing_secret: string;
}
export interface WebhookDelivery {
  id: string;
  webhook_id: string;
  event: string;
  ts: string;
  url: string;
  status_code: number;
  attempt: 1 | 2 | 3;
  latency_ms: number;
  request_body_preview: string;
  response_body_preview: string;
}

// Custom roles / permissions (spec Part I).
export interface PermissionDef {
  key: string;
  group: string;
  description: string;
}
export interface CustomRoleInput {
  name: string;
  description: string;
  permissions: string[];
  default_for_new_users: boolean;
}

// Automation tokens (spec Part M).
export interface AutomationToken {
  id: string;
  name: string;
  prefix: string;
  user_id: string;
  role_at_mint: string;
  permissions: string[];
  expires_at: string | null;
  last_used: string | null;
  created_at: string;
}
export interface CreatedAutomationToken {
  token: AutomationToken;
  plaintext: string;
}

// WebAuthn / passkeys (spec Part K).
export interface WebAuthnCredential {
  id: string;
  label: string;
  created_at: string;
  last_used: string | null;
}

// Backups (spec Part L).
export interface BackupRow {
  id: string;
  taken_at: string;
  version: string;
  size_bytes: number;
  db_sha256: string;
  path: string;
}

// Model aliases (spec Part C).
export interface ModelAlias {
  alias: string;
  concrete_model: string;
  service_id: string;
  created_at: string;
}

// Provisioning (§4.28; pulled forward from v0.3.1).
export interface ProvisioningKey {
  id: string;
  name: string;
  prefix: string;
  scope: "single" | "multi";
  expires_at: string | null;
  default_role: string;
  last_used: string | null;
  created_at: string;
}
export interface ProvisioningPending {
  id: string;
  hostname: string;
  os: string;
  arch: string;
  remote_ip: string;
  provisioning_key_id: string;
  first_seen: string;
}

// Redaction rules (spec Part B.7 / Guardrails).
export interface RedactionRule {
  id: string;
  name: string;
  pattern: string;
  action: "mask" | "drop" | "hash";
  scope: "request_body" | "response_body" | "both";
  builtin?: boolean;
}
