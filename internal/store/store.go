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

// SeedAdmin creates the first admin user when no users exist.
// It is a no-op when email or password is empty, or when any user already exists.
func (s *Store) SeedAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	// CountUsers→CreateUser is safe: runs once at startup before serving and SetMaxOpenConns(1) serialises it.
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return s.q.CreateUser(ctx, db.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		Role:         "admin",
	})
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
