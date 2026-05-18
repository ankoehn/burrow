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
	"github.com/ankoehn/burrow/internal/db"
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
// The caller retains ownership of the underlying *sql.DB and must close it (Store has no Close).
type Store struct{ q *db.DB }

// New builds a Store over an open, migrated *sql.DB.
func New(d *sql.DB) *Store { return &Store{q: db.Wrap(d)} }

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

// ListUsers returns all users (id, email, role, created_at — no password_hash).
func (s *Store) ListUsers(ctx context.Context) ([]db.User, error) {
	return s.q.ListUsers(ctx)
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
