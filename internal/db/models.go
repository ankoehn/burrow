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

// Role is a row of the roles table (built-in only in v0.2.0).
type Role struct {
	Name, Description string
	CreatedAt         time.Time
}

// Setting is a row of the settings key/value table.
type Setting struct {
	Key, Value string
	UpdatedAt  time.Time
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
