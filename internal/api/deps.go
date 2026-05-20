// Package api is the Burrow HTTP JSON API (chi router, cookie sessions, SSE).
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// ServiceStore is the subset of internal/store the service handlers need.
// *store.Store satisfies it implicitly.
type ServiceStore interface {
	ListServices(ctx context.Context, callerID, callerRole string) ([]store.ServiceView, error)
	GetService(ctx context.Context, callerID, callerRole, serviceID string) (store.ServiceDetail, error)
	SetServiceAccessMode(ctx context.Context, callerID, callerRole, serviceID, mode, header string) error
	ListAPIKeys(ctx context.Context, callerID, callerRole, serviceID string) ([]db.ServiceAPIKey, error)
	CreateAPIKey(ctx context.Context, callerID, callerRole, serviceID, name string) (id, plaintext string, err error)
	DeleteAPIKey(ctx context.Context, callerID, callerRole, serviceID, keyID string) error
	GetAccessPolicy(ctx context.Context, callerID, callerRole, serviceID string) ([]string, error)
	SetAccessPolicy(ctx context.Context, callerID, callerRole, serviceID string, roles []string) error
}

// LiveTunnelSnapshot is the live/runtime subset of a tunnel that the API
// composes into service responses. It deliberately contains only the fields
// the handlers need, keeping the interface narrow and test-friendly.
type LiveTunnelSnapshot struct {
	LocalAddr  string
	Connected  bool
	RemotePort int // non-zero for tcp services; 0 for http services
}

// TunnelLocator holds the service and user association for a live tunnel,
// used by the v0.2 PUT /tunnels/{id}/access-mode back-compat path to resolve
// a tunnelID → serviceID before delegating to the service store.
type TunnelLocator struct {
	ServiceID string
	UserID    string
}

// LiveTunnelLookup is the narrow interface the service handlers use to query
// the in-memory tunnel registry for live runtime state. The concrete
// *server.Server (or an adapter) satisfies this; tests provide a fake.
// Task 12 (cmd/server wiring) will inject the real implementation.
type LiveTunnelLookup interface {
	// LookupByServiceID returns the live snapshot for the service with the
	// given serviceID. ok is false when no connected tunnel is registered for
	// that service.
	LookupByServiceID(serviceID string) (LiveTunnelSnapshot, bool)
	// LookupByTunnelID resolves a tunnel's runtime ID to its service + user.
	// ok is false when no live tunnel with that ID is registered.
	LookupByTunnelID(tunnelID string) (TunnelLocator, bool)
}

// UserStore is the subset of internal/store the API needs. *store.Store satisfies it.
type UserStore interface {
	VerifyUserPassword(ctx context.Context, email, password string) (bool, error)
	// GetUserByEmail is used by the login handler after VerifyUserPassword (spec F3).
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	GetUserByID(ctx context.Context, id string) (db.User, error)
	IssueClientToken(ctx context.Context, userID, name string) (string, error)
	ListClientTokens(ctx context.Context, userID string) ([]db.ClientToken, error)
	RevokeClientToken(ctx context.Context, id, userID string) error
	CreateSession(ctx context.Context, userID, ua, ip string) (string, error)
	ValidateSession(ctx context.Context, id string) (string, error)
	DeleteSession(ctx context.Context, id string) error
	// Multi-user methods.
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error
	ListUsersPage(ctx context.Context, q string, limit, offset int) ([]db.User, int, error)
	CreateUser(ctx context.Context, email, password, role string) (db.User, error)
	DeleteUser(ctx context.Context, id string) error
	UpdateUserRole(ctx context.Context, id, role string) error
	SetUserStatus(ctx context.Context, id, status string) error
	TouchUserLastLogin(ctx context.Context, id string) error
}

// RoleStore is the roles read surface (built-in roles + code permissions).
type RoleStore interface {
	ListRoles(ctx context.Context) ([]db.Role, error)
	GetRole(ctx context.Context, name string) (store.RoleDetail, error)
}

// SessionStore is the per-user session list/revoke surface.
type SessionStore interface {
	ListSessions(ctx context.Context, userID string) ([]db.Session, error)
	RevokeSession(ctx context.Context, id, userID string) error
	RevokeOtherSessions(ctx context.Context, userID, keepID string) (int64, error)
}

// SettingsStore is the admin settings + SMTP test surface.
type SettingsStore interface {
	GetSettings(ctx context.Context) (map[string]string, error)
	SaveSettings(ctx context.Context, kv map[string]string) error
	SendTestEmail(ctx context.Context, to string) error
}

// Pinger lets /readyz verify the database is reachable.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// ClientView is one live client (control session) for the overview.
type ClientView struct {
	SessionID     string `json:"session_id"`
	UserID        string `json:"user_id"`
	TokenName     string `json:"token_name"`
	RemoteAddr    string `json:"remote_addr"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	ClientVersion string `json:"client_version"`
	ServiceCount  int    `json:"service_count"`
	TotalBytesIn  int64  `json:"total_bytes_in"`
	TotalBytesOut int64  `json:"total_bytes_out"`
}

// ClientServiceView is one service (tunnel) under a client: live + persisted
// byte counters and the per-service access mode.
type ClientServiceView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	RemotePort    int    `json:"remote_port"`
	LocalAddr     string `json:"local_addr"`
	AccessMode    string `json:"access_mode"`
	BytesIn       uint64 `json:"bytes_in"`        // live, since connect
	BytesOut      uint64 `json:"bytes_out"`       // live, since connect
	TotalBytesIn  int64  `json:"total_bytes_in"`  // persisted, survives reconnect
	TotalBytesOut int64  `json:"total_bytes_out"` // persisted
}

// ClientDetail is a client plus its services.
type ClientDetail struct {
	ClientView
	Services []ClientServiceView `json:"services"`
}

// ClientLister exposes live client sessions + their services (cmd/server
// adapts the registry + store totals). See client_handlers.go.
type ClientLister interface {
	ListClients() []ClientView
	GetClient(sessionID string) (ClientDetail, bool)
}

// AccessModeSetter sets a tunnel's per-service access mode (scoped to owner).
type AccessModeSetter interface {
	SetTunnelAccessMode(ctx context.Context, id, userID, mode string) error
}

// TunnelView is one live tunnel as exposed by the API.
type TunnelView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	RemotePort int    `json:"remote_port"`
	LocalAddr  string `json:"local_addr"`
	BytesIn    uint64 `json:"bytes_in"`
	BytesOut   uint64 `json:"bytes_out"`
	Connected  bool   `json:"connected"`
}

// TunnelLister returns the live tunnels owned by a user (cmd/server adapts the registry).
type TunnelLister interface {
	ListUserTunnels(userID string) []TunnelView
}

// EventStream is the SSE subscription side of the events bus. *events.Bus satisfies it.
type EventStream interface {
	Subscribe(userID string) (<-chan struct{}, func())
}

// LoginRateLimitPerIP is the default maximum login attempts per IP per minute.
// Per-IP accuracy is guaranteed by TrustedProxyMiddleware (C2), which runs
// before the limiter and gates XFF trust behind a trusted-CIDR allowlist.
const LoginRateLimitPerIP = 10

// LoginRateLimitGlobal is the default maximum login attempts across all IPs
// per minute. This global cap bounds concurrent argon2id CPU/RAM cost.
const LoginRateLimitGlobal = 60

// Deps are the API's injected dependencies.
type Deps struct {
	Users         UserStore
	Tunnels       TunnelLister
	Events        EventStream
	Log           *slog.Logger
	SecureCookies bool
	// HTTPSEnabled signals that the server itself is serving TLS natively
	// (BURROW_HTTP_TLS_CERT/KEY both set). When true:
	//   - The HSTSMiddleware adds Strict-Transport-Security to every response.
	//   - SecureCookies is forced on, regardless of the http_secure_cookies setting.
	// This flag is set by cmd/server and MUST NOT be derived from spoofable
	// headers such as X-Forwarded-Proto.
	HTTPSEnabled bool
	// SPA, if non-nil, serves the embedded dashboard for any non-/api/v1 path
	// (client-side routing). Nil keeps pure-API behavior (Phase 4b).
	SPA http.Handler
	// TrustedProxies is the list of CIDRs/IPs whose X-Forwarded-For /
	// X-Real-IP headers the server will honor. Empty means no forwarded
	// headers are trusted (safe default). Populated from config.ServerConfig
	// by cmd/server. See TrustedProxyMiddleware.
	TrustedProxies []string
	// LoginRateLimitPerIP overrides LoginRateLimitPerIP for tests; zero uses the const.
	LoginRateLimitPerIPOverride int
	// LoginRateLimitGlobalOverride overrides LoginRateLimitGlobal for tests; zero uses the const.
	LoginRateLimitGlobalOverride int
	// v0.2.0 surfaces.
	Roles       RoleStore
	Sessions    SessionStore
	Settings    SettingsStore
	Clients     ClientLister
	AccessModes AccessModeSetter
	DB          Pinger
	// v0.3.0 surfaces.
	// Services is the durable service store (api keys, access mode, policy).
	Services ServiceStore
	// LiveTunnels allows the service handlers to compose live/runtime fields
	// (connected, local_addr) into service responses. May be nil before
	// Task 12 wires the concrete server.Server implementation.
	LiveTunnels LiveTunnelLookup
	// AuthDomain is the configured auth domain (e.g. "tunnels.example.com").
	// Empty means no auth domain is configured; burrow_login mode is rejected
	// with 409 when this field is empty.
	AuthDomain string

	// v0.4.0 surfaces — additive; nil-safe handlers degrade gracefully.
	// CacheEngine is the exact-match prompt cache (clear/stats surface for
	// the JSON API; Lookup/Store live on the proxy hot path wired separately).
	// May be nil before Task 12 wires the concrete *exact.Cache.
	CacheEngine CacheEngine
	// CacheServices is the per-service ai-config lookup surface used by the
	// per-service cache endpoints (DELETE /services/{id}/cache/entries,
	// GET /cache/settings per_service list). May be nil before Task 24 wires
	// the typed ServiceAIConfig store.
	CacheServices CacheServiceLookup
	// ModelAliases is the CRUD surface for the model_aliases table (spec
	// Part C.1: GET/POST/PUT/DELETE /api/v1/models/aliases). *db.DB satisfies
	// it; nil disables the four routes (handlers return 500 with a clear
	// "alias store unavailable" body).
	ModelAliases ModelAliasStore
	// InspectorRings is the per-service in-memory ring-buffer manager
	// (spec Part E). *inspector.Manager satisfies it. When nil, the
	// list/get routes degrade to empty / 404 and the stream returns 500.
	InspectorRings InspectorRings
	// InspectorServices is the narrow service-ownership lookup used by the
	// inspector handlers' :own permission gate. *store.Store satisfies it
	// via GetServiceOwner; *db.DB exposes the same name. Nil disables :own
	// checks (handlers return 500 "service lookup unavailable" for :own
	// callers; :any callers still pass).
	InspectorServices InspectorOwnerLookup
	// InspectorReplayer re-fires an inspector entry's request through the
	// proxy chain. Wired in Task 10/25; nil keeps the routes registered
	// but the POST .../replay and POST .../replay-compare handlers return
	// 503 "replay engine unavailable".
	InspectorReplayer InspectorReplayer
	// RateLimitDB is the CRUD surface for the rate_limits table (spec
	// Part D.2: GET/POST/PUT/DELETE /api/v1/rate-limits). *db.DB satisfies
	// it; nil disables the four routes (handlers return 500 with a clear
	// "rate-limit store unavailable" body).
	RateLimitDB RateLimitStore
	// RateLimits is the runtime engine consulted by the proxy chain and
	// (read-side) by the /rate-limits/usage endpoint. Mutations to the
	// configured rule set call Reload synchronously so the next charge
	// sees the new shape. Wired in cmd/server (Task 12); nil keeps the
	// API routes functional but Charge always allows.
	RateLimits QuotaEngine
	// Budgets is the CRUD + usage-aggregation surface for the budgets
	// table (spec Part F). *db.DB satisfies it; nil disables all of
	// /api/v1/budgets and /api/v1/cost/export (handlers return 500 with a
	// clear "budget store unavailable" body).
	Budgets BudgetStore
	// CostEngine is the in-process pricing-table + budget-trigger engine
	// (spec Part F). Backs GET/PUT /api/v1/cost/pricing and the live
	// current_usd field returned by GET /budgets. Wired in cmd/server
	// (Task 25); nil makes GET /cost/pricing return an empty table and
	// PUT return 500, while GET /budgets still returns the configured
	// rows with current_usd=0.
	CostEngine CostEngine
	// AuditEvents is the read surface backing GET /audit/events (the
	// admin UI's audit log table) and POST /audit/verify (for resolving
	// first_id/last_id within a range). *db.DB satisfies it. Nil disables
	// the routes (handlers return 500 "audit store unavailable").
	AuditEvents AuditQueryStore
	// AuditChain is the action surface: verify, export, public-key. Wraps
	// an *audit.Logger via NewAuditChainAdapter. cmd/server constructs it
	// after LoadOrGenerateSigningKey wires the key. Nil disables verify /
	// export / fingerprint.
	AuditChain AuditChain
}

type ctxKey int

const userIDKey ctxKey = 0

// userID returns the authenticated user id stored by RequireSession, or "" if
// the context has none (safe default; every authenticated route MUST be guarded
// by RequireSession — public handlers calling this get "").
func userID(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}
