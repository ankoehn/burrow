// Package api is the Burrow HTTP JSON API (chi router, cookie sessions, SSE).
package api

import (
	"context"
	"log/slog"

	"github.com/ankoehn/burrow/internal/db"
)

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

// Deps are the API's injected dependencies.
type Deps struct {
	Users         UserStore
	Tunnels       TunnelLister
	Events        EventStream
	Log           *slog.Logger
	SecureCookies bool
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
