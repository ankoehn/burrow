// Package store composes the db layer + auth into the interfaces the server
// and (later) the HTTP API depend on.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/ankoehn/burrow/internal/auth"
	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/mailer"
)

// ErrUnauthorized is returned when a client token does not match.
var ErrUnauthorized = errors.New("store: unauthorized")

// ErrInvalidCredentials is returned by ChangePassword when the current password is wrong.
var ErrInvalidCredentials = errors.New("store: invalid credentials")

// ErrPasswordTooShort is returned when a new password is shorter than minPasswordLen.
var ErrPasswordTooShort = errors.New("store: password too short (minimum 8 characters)")

// ErrEmailConflict is returned by CreateUser when the email already exists.
var ErrEmailConflict = errors.New("store: email already in use")

// ErrInvalidRole is returned by CreateUser when the role is not 'admin' or 'user'.
var ErrInvalidRole = errors.New("store: role must be 'admin' or 'user'")

// ErrInvalidStatus is returned by SetUserStatus for a status other than
// 'active' or 'suspended'.
var ErrInvalidStatus = errors.New("store: status must be 'active' or 'suspended'")

// ErrInvalidAccessMode is returned by SetTunnelAccessMode for a value other
// than 'open', 'api_key', or 'burrow_login'.
var ErrInvalidAccessMode = errors.New("store: access_mode must be 'open', 'api_key', or 'burrow_login'")

// ErrSMTPUnconfigured is returned by SendTestEmail when no smtp.host is set.
var ErrSMTPUnconfigured = errors.New("store: smtp is not configured")

// minPasswordLen is the minimum required password length.
const minPasswordLen = 8

// sessionTTL is the lifetime of a browser session.
const sessionTTL = 7 * 24 * time.Hour

// SaveTunnelArg is the subset of a tunnel the store persists.
// The server adapter in cmd/server converts *server.Tunnel to this type,
// keeping store free of any import of internal/server.
type SaveTunnelArg struct {
	ID, Name, Type, LocalAddr string
	RemotePort                int
}

// Store is the DB-backed implementation of the server/API dependencies.
// The caller retains ownership of the underlying *sql.DB (Store has no Close).
type Store struct {
	q            *db.DB
	smtpPassword string // injected from BURROW_SMTP_PASSWORD(_FILE); never persisted
}

// New builds a Store over an open, migrated *sql.DB.
func New(d *sql.DB) *Store { return &Store{q: db.Wrap(d)} }

// SetSMTPPassword injects the SMTP secret (from config). Called by cmd/server.
func (s *Store) SetSMTPPassword(pw string) { s.smtpPassword = pw }

// SeedAdmin ensures the bootstrap admin user exists.
// It is a no-op when email or password is empty (unset BURROW_ADMIN_* — safe).
// Uses INSERT ... ON CONFLICT(email) DO NOTHING so the operation is
// idempotent and race-proof: the UNIQUE(email) constraint in the schema
// guarantees atomicity — two concurrent callers both succeed without
// duplication (one inserts, the other gets 0 rows affected and returns nil).
// B17: eliminates the prior check-then-insert pattern.
func (s *Store) SeedAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.q.DB().ExecContext(ctx,
		`INSERT INTO users(id, email, password_hash, role)
		 VALUES(?,?,?,'admin')
		 ON CONFLICT(email) DO NOTHING`,
		uuid.NewString(), email, hash,
	)
	return err
}

// VerifyUserPassword checks whether the given password matches the stored hash
// for the user identified by email. Returns (false, nil) for unknown emails
// so that login of an unknown email is a normal "wrong creds", not a 500.
func (s *Store) VerifyUserPassword(ctx context.Context, email, password string) (bool, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return auth.VerifyPassword(password, u.PasswordHash)
}

// GetUserByEmail returns the user with the given email, or db.ErrNotFound.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (db.User, error) {
	return s.q.GetUserByEmail(ctx, email)
}

// GetUserByID returns the user with the given ID, or db.ErrNotFound.
func (s *Store) GetUserByID(ctx context.Context, id string) (db.User, error) {
	return s.q.GetUserByID(ctx, id)
}

// IssueClientToken generates a new client token for the given user and name,
// persists the hash, and returns the plaintext (shown once).
func (s *Store) IssueClientToken(ctx context.Context, userID, name string) (plaintext string, err error) {
	pt, hash, err := auth.GenerateClientToken()
	if err != nil {
		return "", err
	}
	if err := s.q.CreateClientToken(ctx, db.ClientToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
	}); err != nil {
		return "", err
	}
	return pt, nil
}

// ListClientTokens returns all client tokens belonging to the given user.
func (s *Store) ListClientTokens(ctx context.Context, userID string) ([]db.ClientToken, error) {
	return s.q.ListClientTokensByUser(ctx, userID)
}

// RevokeClientToken deletes the token with the given id, scoped to owning user.
func (s *Store) RevokeClientToken(ctx context.Context, id, userID string) error {
	return s.q.DeleteClientToken(ctx, id, userID)
}

// Authenticate validates a plaintext client token and returns the owning user's ID.
// Returns ErrUnauthorized for unknown or invalid tokens.
// The last-used timestamp update is best-effort and does not fail authentication.
func (s *Store) Authenticate(ctx context.Context, plaintext string) (userID string, err error) {
	hash := auth.HashToken(plaintext)
	ct, err := s.q.GetClientTokenByHash(ctx, hash)
	if errors.Is(err, db.ErrNotFound) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	// Best-effort: update last_used without failing auth on error.
	_ = s.q.TouchClientTokenLastUsed(ctx, ct.ID)
	return ct.UserID, nil
}

// AuthenticateNamed is like Authenticate but also returns the client-token's
// name (used by the server to label clients in the overview). Returns
// ErrUnauthorized for unknown/invalid tokens.
func (s *Store) AuthenticateNamed(ctx context.Context, plaintext string) (userID, tokenName string, err error) {
	hash := auth.HashToken(plaintext)
	ct, err := s.q.GetClientTokenByHash(ctx, hash)
	if errors.Is(err, db.ErrNotFound) {
		return "", "", ErrUnauthorized
	}
	if err != nil {
		return "", "", err
	}
	_ = s.q.TouchClientTokenLastUsed(ctx, ct.ID)
	return ct.UserID, ct.Name, nil
}

// SaveTunnel upserts a tunnel row belonging to the given user.
func (s *Store) SaveTunnel(ctx context.Context, userID string, t *SaveTunnelArg) error {
	return s.q.UpsertTunnel(ctx, db.Tunnel{
		ID:         t.ID,
		UserID:     userID,
		Name:       t.Name,
		Type:       t.Type,
		RemotePort: t.RemotePort,
		LocalAddr:  t.LocalAddr,
	})
}

// MarkTunnelSeen updates the last_seen timestamp for the given tunnel ID.
func (s *Store) MarkTunnelSeen(ctx context.Context, tunnelID string) error {
	return s.q.TouchTunnelLastSeen(ctx, tunnelID)
}

// CreateSession creates a new browser session for the given user, returning
// the session ID. The session expires after 7 days.
func (s *Store) CreateSession(ctx context.Context, userID, ua, ip string) (id string, err error) {
	id = uuid.NewString()
	if err := s.q.CreateSession(ctx, db.Session{
		ID:        id,
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(sessionTTL),
		UserAgent: ua,
		IP:        ip,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// ValidateSession looks up a session by ID and returns the owning user ID.
// Returns ErrUnauthorized if the session does not exist or has expired.
// Expired sessions are deleted best-effort before returning.
func (s *Store) ValidateSession(ctx context.Context, id string) (userID string, err error) {
	sess, err := s.q.GetSession(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		_ = s.q.DeleteSession(ctx, id)
		return "", ErrUnauthorized
	}
	return sess.UserID, nil
}

// DeleteSession removes the session with the given ID.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.q.DeleteSession(ctx, id)
}

// ChangePassword verifies currentPassword, enforces minimum length on newPassword,
// re-hashes, and persists. Returns ErrInvalidCredentials on wrong current password,
// ErrPasswordTooShort on a short new password. Sessions are NOT rotated (MVP decision).
func (s *Store) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := auth.VerifyPassword(currentPassword, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	if len(newPassword) < minPasswordLen {
		return ErrPasswordTooShort
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.q.UpdateUserPassword(ctx, userID, hash)
}

// UpdateUserPassword is a thin wrapper that updates the password hash directly.
// Callers that need to enforce business rules (current password check, min-length)
// should use ChangePassword instead.
func (s *Store) UpdateUserPassword(ctx context.Context, userID, newHash string) error {
	return s.q.UpdateUserPassword(ctx, userID, newHash)
}

// ListUsersPage returns a filtered, paginated page plus the full filtered total.
func (s *Store) ListUsersPage(ctx context.Context, q string, limit, offset int) ([]db.User, int, error) {
	return s.q.ListUsersPage(ctx, q, limit, offset)
}

// UpdateUserRole validates and sets a user's role.
func (s *Store) UpdateUserRole(ctx context.Context, id, role string) error {
	if role != "admin" && role != "user" {
		return ErrInvalidRole
	}
	return s.q.UpdateUserRole(ctx, id, role)
}

// SetUserStatus validates and sets a user's status. Suspending also revokes
// every existing session for that user (so suspension takes effect immediately
// without adding a per-request user lookup to RequireSession).
func (s *Store) SetUserStatus(ctx context.Context, id, status string) error {
	if status != "active" && status != "suspended" {
		return ErrInvalidStatus
	}
	if err := s.q.UpdateUserStatus(ctx, id, status); err != nil {
		return err
	}
	if status == "suspended" {
		if _, err := s.q.DeleteSessionsByUser(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// TouchUserLastLogin stamps the user's last_login (best-effort at the call site).
func (s *Store) TouchUserLastLogin(ctx context.Context, id string) error {
	return s.q.TouchUserLastLogin(ctx, id)
}

// RoleDetail is a role row enriched with its code-defined permission set.
type RoleDetail struct {
	Name        string
	Description string
	CreatedAt   time.Time
	Permissions []string
}

// ListRoles returns the built-in roles (DB rows for name/description/created_at).
func (s *Store) ListRoles(ctx context.Context) ([]db.Role, error) {
	return s.q.ListRoles(ctx)
}

// GetRole returns a role's DB row plus its authz permission strings.
func (s *Store) GetRole(ctx context.Context, name string) (RoleDetail, error) {
	r, err := s.q.GetRole(ctx, name)
	if err != nil {
		return RoleDetail{}, err
	}
	d := RoleDetail{Name: r.Name, Description: r.Description, CreatedAt: r.CreatedAt}
	if ar, ok := authz.Get(name); ok {
		for _, p := range ar.Permissions {
			d.Permissions = append(d.Permissions, string(p))
		}
	}
	return d, nil
}

// ListSessions returns the user's sessions (newest first).
func (s *Store) ListSessions(ctx context.Context, userID string) ([]db.Session, error) {
	return s.q.ListSessionsByUser(ctx, userID)
}

// RevokeSession deletes one of the user's sessions (scoped).
func (s *Store) RevokeSession(ctx context.Context, id, userID string) error {
	return s.q.DeleteSessionForUser(ctx, id, userID)
}

// RevokeOtherSessions signs the user out everywhere except keepID.
func (s *Store) RevokeOtherSessions(ctx context.Context, userID, keepID string) (int64, error) {
	return s.q.DeleteSessionsByUserExcept(ctx, userID, keepID)
}

// GetSettings returns the settings table as a flat map.
func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.q.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}

// SaveSettings upserts the given non-secret settings.
func (s *Store) SaveSettings(ctx context.Context, kv map[string]string) error {
	return s.q.SetSettings(ctx, kv)
}

// GetTunnel returns one persisted tunnel row, or db.ErrNotFound.
func (s *Store) GetTunnel(ctx context.Context, id string) (db.Tunnel, error) {
	return s.q.GetTunnel(ctx, id)
}

// SetTunnelAccessMode validates the enum and sets a tunnel's access mode
// scoped to its owner. api_key/burrow_login are accepted and persisted but
// have no runtime effect in v0.2.0 (inert until HTTP tunnels land in v0.3).
func (s *Store) SetTunnelAccessMode(ctx context.Context, id, userID, mode string) error {
	switch mode {
	case "open", "api_key", "burrow_login":
	default:
		return ErrInvalidAccessMode
	}
	return s.q.SetTunnelAccessMode(ctx, id, userID, mode)
}

// FlushTunnelTotals adds the given byte deltas to a tunnel's persisted counters.
func (s *Store) FlushTunnelTotals(ctx context.Context, id string, addIn, addOut int64) error {
	return s.q.FlushTunnelTotals(ctx, id, addIn, addOut)
}

// SMTPConfigFromSettings builds a mailer.Config from the settings table plus
// the injected secret. ok=false means SMTP is unconfigured (no host).
func (s *Store) SMTPConfigFromSettings(ctx context.Context) (cfg mailer.Config, ok bool, err error) {
	m, err := s.GetSettings(ctx)
	if err != nil {
		return mailer.Config{}, false, err
	}
	host := m["smtp.host"]
	if host == "" {
		return mailer.Config{}, false, nil
	}
	port := 0
	for _, r := range m["smtp.port"] {
		if r < '0' || r > '9' {
			port = 0
			break
		}
		port = port*10 + int(r-'0')
	}
	tlsMode := mailer.TLSMode(m["smtp.tls"])
	if tlsMode == "" {
		tlsMode = mailer.TLSSTARTTLS
	}
	return mailer.Config{
		Host:     host,
		Port:     port,
		Username: m["smtp.username"],
		Password: s.smtpPassword,
		From:     m["smtp.from"],
		TLS:      tlsMode,
	}, true, nil
}

// SendTestEmail builds the SMTP config from settings (+ injected secret) and
// sends a test message to `to`. Returns ErrSMTPUnconfigured when no host is set.
func (s *Store) SendTestEmail(ctx context.Context, to string) error {
	cfg, ok, err := s.SMTPConfigFromSettings(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSMTPUnconfigured
	}
	return mailer.SendTest(ctx, cfg, to)
}

// CreateUser creates a new user account. Returns ErrEmailConflict on duplicate email,
// ErrPasswordTooShort on a short password, ErrInvalidRole on an unknown role.
func (s *Store) CreateUser(ctx context.Context, email, password, role string) (db.User, error) {
	if role != "admin" && role != "user" {
		return db.User{}, ErrInvalidRole
	}
	if len(password) < minPasswordLen {
		return db.User{}, ErrPasswordTooShort
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return db.User{}, err
	}
	u := db.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		Role:         role,
	}
	if err := s.q.CreateUser(ctx, u); err != nil {
		// modernc sqlite wraps the underlying error; a UNIQUE constraint violation
		// message contains "UNIQUE constraint failed".
		if isUniqueViolation(err) {
			return db.User{}, ErrEmailConflict
		}
		return db.User{}, err
	}
	return u, nil
}

// DeleteUser removes the user and all associated sessions/tokens/tunnels (ON DELETE CASCADE).
// Returns db.ErrNotFound if no such user exists.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.q.DeleteUser(ctx, id)
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint violation.
// modernc/sqlite wraps driver errors as *sqlite.Error or plain fmt.Errorf strings;
// the canonical way to detect them is to inspect the error message string.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return containsStr(err.Error(), "UNIQUE constraint failed")
}

// containsStr is a small helper so we don't import strings in the store package.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && findStr(s, sub)
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
