package db

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned by Get* when no row matches.
var ErrNotFound = errors.New("db: not found")

// User is a row of the users table.
type User struct {
	ID, Email, PasswordHash, Role string
	Status                        string
	LastLogin                     *time.Time
	CreatedAt, UpdatedAt          time.Time
}

// Session is a row of the sessions table.
type Session struct {
	ID, UserID           string
	ExpiresAt, CreatedAt time.Time
	UserAgent, IP        string
}

// ClientToken is a row of the client_tokens table (token_hash only).
type ClientToken struct {
	ID, UserID, Name, TokenHash string
	LastUsed                    *time.Time
	CreatedAt                   time.Time
}

// Tunnel is a persisted tunnel row (live state stays in-memory).
type Tunnel struct {
	ID, UserID, Name, Type, LocalAddr string
	RemotePort                        int
	CreatedAt                         time.Time
	LastSeen                          *time.Time
	TotalBytesIn, TotalBytesOut       int64
	LastFlushedAt                     *time.Time
	AccessMode                        string
}

// Role is a row of the roles table. v0.4.0 extends with custom-role fields.
type Role struct {
	Name, Description  string
	CreatedAt          time.Time
	Builtin            bool
	Permissions        []string // JSON-encoded in roles.permissions
	DefaultForNewUsers bool
}

// Setting is a row of the settings key/value table.
type Setting struct {
	Key, Value string
	UpdatedAt  time.Time
}

// Service is a row of the services table (HTTP-tunnel access configuration).
type Service struct {
	ID           string
	UserID       string
	Name         string
	Type         string // 'http' or 'tcp'
	Subdomain    string // "" for tcp
	AccessMode   string // 'open'|'api_key'|'burrow_login'
	APIKeyHeader string // default "Authorization"
	CreatedAt    time.Time
}

// ServiceAPIKey is a row of the service_api_keys table.
type ServiceAPIKey struct {
	ID        string
	ServiceID string
	Name      string
	KeyHash   string
	LastUsed  *time.Time
	CreatedAt time.Time
}

// --- v0.4.0 model structs (migrations 0004-0010) -----------------------------

// UsageEvent is a row of the usage_events table (AI traffic meter).
type UsageEvent struct {
	ID             string
	ServiceID      string
	APIKeyID       string
	Ts             time.Time
	Kind           string // openai|anthropic|mcp|unknown
	TokensIn       int64
	TokensOut      int64
	BytesIn        int64
	BytesOut       int64
	Streamed       bool
	CacheHit       bool
	UpstreamStatus int
}

// CacheEntry is a row of the cache_entries table (AI response cache).
type CacheEntry struct {
	ID         string
	ScopeKey   string
	KeyHash    string
	Status     int
	Headers    string // JSON
	Body       []byte
	CreatedAt  time.Time
	TTLSeconds int
	LastHitAt  *time.Time
}

// AuditEvent is a row of the audit_events table (hash-chained audit log).
type AuditEvent struct {
	ID           string // ulid
	Ts           time.Time
	ActorID      string
	ActorEmail   string
	Action       string
	SubjectID    string
	SubjectLabel string
	Result       string // ok|denied|error
	SourceIP     string
	UserAgent    string
	RequestID    string
	Payload      string // JSON
	PrevHash     string
	Hash         string
}

// WebAuthnCredential is a row of the webauthn_credentials table.
type WebAuthnCredential struct {
	ID         string // base64url credential id
	UserID     string
	Label      string
	PublicKey  []byte
	SignCount  int64
	AAGUID     *string
	Transports *string
	CreatedAt  time.Time
	LastUsed   *time.Time
}

// Webhook is a row of the webhooks table.
type Webhook struct {
	ID                  string
	Name                string
	URL                 string
	SecretHash          string
	Events              string // JSON array
	Paused              bool
	ConsecutiveFailures int
	FirstFailureAt      *time.Time
	CreatedAt           time.Time
}

// WebhookDelivery is a row of the webhook_deliveries table.
type WebhookDelivery struct {
	ID              string
	WebhookID       string
	Event           string
	Ts              time.Time
	StatusCode      int
	Attempt         int
	LatencyMs       int
	RequestPreview  *string
	ResponsePreview *string
}

// ServiceAIConfig is a row of the service_ai_config table.
type ServiceAIConfig struct {
	ServiceID string
	Config    string // JSON (ServiceAIConfig per spec Part B.7)
	UpdatedAt time.Time
}

// ModelAlias is a row of the model_aliases table.
type ModelAlias struct {
	Alias         string
	ConcreteModel string
	ServiceID     string
	CreatedAt     time.Time
}

// RateLimit is a row of the rate_limits table.
type RateLimit struct {
	ID        string
	Scope     string // api_key|role|service|global
	Subject   string
	Dimension string // rpm|bpm
	Lim       int64
	Burst     int64
	Window    string // minute|day
	CreatedAt time.Time
}

// Budget is a row of the budgets table.
type Budget struct {
	ID              string
	Scope           string // api_key|service|user|global
	SubjectID       string
	DailyUSD        float64
	ActionOnExceed  string // alert_webhook|throttle_zero|disable_key
	AlertWebhookID  *string
	CreatedAt       time.Time
}

// ServiceIPGeo is a row of the service_ip_geo table.
type ServiceIPGeo struct {
	ServiceID      string
	Enabled        bool
	AllowCIDRs     string // JSON array
	BlockCIDRs     string // JSON array
	AllowCountries string // JSON array
	BlockCountries string // JSON array
}

// AutomationToken is a row of the automation_tokens table.
type AutomationToken struct {
	ID          string
	Name        string
	Prefix      string
	UserID      string
	RoleAtMint  string
	TokenHash   string
	Permissions string // JSON array
	ExpiresAt   *time.Time
	LastUsed    *time.Time
	CreatedAt   time.Time
}

// DB wraps *sql.DB and exposes typed CRUD methods for Burrow's tables.
type DB struct {
	sqlDB *sql.DB
}

// Wrap wraps an existing *sql.DB (already opened and migrated) in the typed DB.
func Wrap(d *sql.DB) *DB {
	return &DB{sqlDB: d}
}

// DB returns the underlying *sql.DB.
func (x *DB) DB() *sql.DB {
	return x.sqlDB
}

// Close closes the underlying database connection.
func (x *DB) Close() error {
	return x.sqlDB.Close()
}
