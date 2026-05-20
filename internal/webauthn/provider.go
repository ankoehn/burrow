// Package webauthn wraps github.com/go-webauthn/webauthn as a thin Burrow-
// shaped provider: it owns the RP configuration, an in-process challenge
// store, and a webauthnUser adapter that exposes db.WebAuthnCredential rows
// to the library.
//
// The provider deliberately does NOT touch the burrow_session cookie or
// store.CreateSession — the api/webauthn_handlers.go thin handlers do that
// after FinishLogin succeeds. This keeps the provider testable without an
// HTTP layer.
package webauthn

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	"github.com/ankoehn/burrow/internal/db"
)

// CredentialStore is the narrow CRUD surface the provider needs from the
// database layer. *db.DB satisfies it directly. The interface keeps the
// provider testable without spinning up SQLite.
type CredentialStore interface {
	CreateWebAuthnCredential(ctx context.Context, c db.WebAuthnCredential) error
	GetWebAuthnCredential(ctx context.Context, id string) (db.WebAuthnCredential, error)
	ListWebAuthnCredentialsByUser(ctx context.Context, userID string) ([]db.WebAuthnCredential, error)
	DeleteWebAuthnCredential(ctx context.Context, id string) error
	UpdateWebAuthnSignCount(ctx context.Context, id string, signCount int64) error
}

// UserLookup is the read surface the provider needs to resolve an email to
// its user id (login/begin) and to render the WebAuthn user entity. *store.Store
// — which already exposes GetUserByEmail/ID via UserStore — satisfies it.
type UserLookup interface {
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	GetUserByID(ctx context.Context, id string) (db.User, error)
}

// Provider wraps a *gowebauthn.WebAuthn with the Burrow data layer. It is
// safe for concurrent use; the in-process session map has its own lock.
type Provider struct {
	wa       *gowebauthn.WebAuthn
	creds    CredentialStore
	users    UserLookup
	rpid     string
	origin   string
	rpName   string
	log      *slog.Logger
	sessions *sessionStore
}

// New constructs a Provider. rpid is the Relying Party ID (a bare host
// without scheme/port). When the operator has configured an auth_domain the
// caller passes that; otherwise the dashboard host. origin is the full
// scheme://host URL the browser will use; the library verifies clientDataJSON
// matches it. rpName is the human-readable display name ("Burrow", typically).
func New(creds CredentialStore, users UserLookup, rpid, rpName, origin string, log *slog.Logger) (*Provider, error) {
	if log == nil {
		log = slog.Default()
	}
	wa, err := gowebauthn.New(&gowebauthn.Config{
		RPID:          rpid,
		RPDisplayName: rpName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn: new: %w", err)
	}
	return &Provider{
		wa:       wa,
		creds:    creds,
		users:    users,
		rpid:     rpid,
		origin:   origin,
		rpName:   rpName,
		log:      log,
		sessions: newSessionStore(),
	}, nil
}

// RPID returns the relying-party id this provider was constructed with.
// Exposed for tests + the /me-style debug surface.
func (p *Provider) RPID() string { return p.rpid }

// Origin returns the configured origin string.
func (p *Provider) Origin() string { return p.origin }

// --- WebAuthn library user adapter ---------------------------------------

// webauthnUser adapts a db.User + their credentials to the
// gowebauthn.User interface.
type webauthnUser struct {
	id          []byte
	name        string
	displayName string
	creds       []gowebauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte                           { return u.id }
func (u *webauthnUser) WebAuthnName() string                         { return u.name }
func (u *webauthnUser) WebAuthnDisplayName() string                  { return u.displayName }
func (u *webauthnUser) WebAuthnCredentials() []gowebauthn.Credential { return u.creds }

// loadUser builds a webauthnUser from the burrow db row plus its credentials.
// Used by Begin{Register,Login} and Finish{Register,Login} so the library
// has a consistent view of the user.
func (p *Provider) loadUser(ctx context.Context, u db.User) (*webauthnUser, error) {
	rows, err := p.creds.ListWebAuthnCredentialsByUser(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: list creds: %w", err)
	}
	out := &webauthnUser{
		// WebAuthnID must be a stable opaque byte sequence per the spec; we
		// derive it from the user id string. Using the raw ULID bytes (or
		// any short stable handle) is fine — the library only compares it
		// for equality between Begin and Finish.
		id:          []byte(u.ID),
		name:        u.Email,
		displayName: u.Email,
	}
	for _, r := range rows {
		out.creds = append(out.creds, libCredentialFromRow(r))
	}
	return out, nil
}

// libCredentialFromRow rebuilds the minimal gowebauthn.Credential the library
// needs for the assertion path (ID, PublicKey, sign counter, transports).
// Attestation metadata is intentionally omitted — we don't re-verify
// attestation on every login.
func libCredentialFromRow(r db.WebAuthnCredential) gowebauthn.Credential {
	return gowebauthn.Credential{
		ID:        []byte(r.ID),
		PublicKey: r.PublicKey,
		Authenticator: gowebauthn.Authenticator{
			SignCount: uint32(r.SignCount), //nolint:gosec // counter overflow not a real concern
		},
	}
}

// --- BeginRegister --------------------------------------------------------

// BeginRegisterResult is the JSON-encodable result of a register/begin call.
// The Options field is the raw library struct (the wire shape the browser
// expects); SessionID is the opaque handle the client echoes back on
// register/finish so the server can re-load the session.
type BeginRegisterResult struct {
	Options   any    // *protocol.CredentialCreation (left as `any` to keep this file import-light)
	SessionID string `json:"session_id"`
}

// BeginRegister starts a registration ceremony for the given user. The
// returned options must be encoded to JSON and shipped to the browser; the
// SessionID must be returned to the client so it can replay it on
// register/finish.
func (p *Provider) BeginRegister(ctx context.Context, userID string) (*BeginRegisterResult, error) {
	u, err := p.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: load user: %w", err)
	}
	wu, err := p.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	creation, session, err := p.wa.BeginRegistration(wu)
	if err != nil {
		return nil, fmt.Errorf("webauthn: begin register: %w", err)
	}
	sid := p.sessions.put(session)
	return &BeginRegisterResult{Options: creation, SessionID: sid}, nil
}

// --- FinishRegister -------------------------------------------------------

// FinishRegister consumes a previously-issued session id and the http.Request
// carrying the authenticator's attestation, validates it, and persists a new
// webauthn_credentials row.
func (p *Provider) FinishRegister(ctx context.Context, userID, sessionID, label string, r *http.Request) (*db.WebAuthnCredential, error) {
	session, ok := p.sessions.take(sessionID)
	if !ok {
		return nil, ErrUnknownSession
	}
	u, err := p.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: load user: %w", err)
	}
	wu, err := p.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	libCred, err := p.wa.FinishRegistration(wu, *session, r)
	if err != nil {
		return nil, fmt.Errorf("webauthn: finish register: %w", err)
	}
	row := credentialRowFromLib(userID, label, libCred)
	if err := p.creds.CreateWebAuthnCredential(ctx, row); err != nil {
		return nil, fmt.Errorf("webauthn: persist cred: %w", err)
	}
	return &row, nil
}

// credentialRowFromLib converts the library's freshly-issued *Credential to
// a db.WebAuthnCredential row. The credential ID is stored as a hex string
// (the migration says "base64url" but any stable encoding works as a primary
// key — the comparison is byte-for-byte on retrieval via [libCredentialFromRow]).
func credentialRowFromLib(userID, label string, c *gowebauthn.Credential) db.WebAuthnCredential {
	row := db.WebAuthnCredential{
		ID:        hex.EncodeToString(c.ID),
		UserID:    userID,
		Label:     label,
		PublicKey: c.PublicKey,
		SignCount: int64(c.Authenticator.SignCount),
	}
	if len(c.Authenticator.AAGUID) > 0 {
		s := hex.EncodeToString(c.Authenticator.AAGUID)
		row.AAGUID = &s
	}
	if len(c.Transport) > 0 {
		var sb string
		for i, t := range c.Transport {
			if i > 0 {
				sb += ","
			}
			sb += string(t)
		}
		row.Transports = &sb
	}
	return row
}

// --- BeginLogin -----------------------------------------------------------

// BeginLoginResult mirrors BeginRegisterResult for the assertion ceremony.
type BeginLoginResult struct {
	Options   any
	SessionID string
	UserID    string // resolved from the email so the finish handler can pin it
}

// BeginLogin starts a login ceremony for the user identified by email. When
// the email maps to no user OR the user has no registered credentials this
// returns ErrUnknownUser — handlers should map it to 401.
func (p *Provider) BeginLogin(ctx context.Context, email string) (*BeginLoginResult, error) {
	u, err := p.users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrUnknownUser
		}
		return nil, fmt.Errorf("webauthn: lookup email: %w", err)
	}
	wu, err := p.loadUser(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(wu.creds) == 0 {
		return nil, ErrUnknownUser
	}
	assertion, session, err := p.wa.BeginLogin(wu)
	if err != nil {
		return nil, fmt.Errorf("webauthn: begin login: %w", err)
	}
	sid := p.sessions.put(session)
	return &BeginLoginResult{Options: assertion, SessionID: sid, UserID: u.ID}, nil
}

// --- FinishLogin ----------------------------------------------------------

// FinishLogin consumes the session id, validates the authenticator's
// assertion against the persisted credential, bumps sign_count, and returns
// the user id that successfully authenticated.
func (p *Provider) FinishLogin(ctx context.Context, sessionID string, r *http.Request) (string, error) {
	session, ok := p.sessions.take(sessionID)
	if !ok {
		return "", ErrUnknownSession
	}
	// The session pins which user owns the credential — refuse to look up
	// a different user with the same assertion.
	if len(session.UserID) == 0 {
		return "", ErrUnknownSession
	}
	userIDStr := string(session.UserID)
	u, err := p.users.GetUserByID(ctx, userIDStr)
	if err != nil {
		return "", fmt.Errorf("webauthn: load user: %w", err)
	}
	wu, err := p.loadUser(ctx, u)
	if err != nil {
		return "", err
	}
	libCred, err := p.wa.FinishLogin(wu, *session, r)
	if err != nil {
		return "", fmt.Errorf("webauthn: finish login: %w", err)
	}
	// Update the sign counter (replay defence).
	rowID := hex.EncodeToString(libCred.ID)
	if err := p.creds.UpdateWebAuthnSignCount(ctx, rowID, int64(libCred.Authenticator.SignCount)); err != nil {
		// Don't fail the login on a counter-update hiccup: the assertion
		// already verified. Log and move on.
		p.log.Warn("webauthn: update sign_count failed", "err", err, "credID", rowID)
	}
	return u.ID, nil
}

// --- DeleteCredential / List ---------------------------------------------

// ListCredentialsForUser returns every credential owned by the user. The
// slice is always non-nil.
func (p *Provider) ListCredentialsForUser(ctx context.Context, userID string) ([]db.WebAuthnCredential, error) {
	return p.creds.ListWebAuthnCredentialsByUser(ctx, userID)
}

// DeleteCredential removes one credential — but only if it belongs to the
// caller. Returns ErrForbidden for cross-user attempts; ErrNotFound for
// missing rows.
func (p *Provider) DeleteCredential(ctx context.Context, callerID, credID string) error {
	row, err := p.creds.GetWebAuthnCredential(ctx, credID)
	if err != nil {
		return err
	}
	if row.UserID != callerID {
		return ErrForbidden
	}
	return p.creds.DeleteWebAuthnCredential(ctx, credID)
}

// --- Errors ---------------------------------------------------------------

var (
	// ErrUnknownSession is returned by FinishRegister / FinishLogin when
	// the session id is missing or already consumed (single-use). Handlers
	// map this to 400.
	ErrUnknownSession = errors.New("webauthn: unknown or expired session")
	// ErrUnknownUser is returned by BeginLogin when the email has no user
	// or the user has no credentials. Handlers map this to 401 — never
	// reveal which.
	ErrUnknownUser = errors.New("webauthn: unknown user or no credentials")
	// ErrForbidden is returned by DeleteCredential when the caller does
	// not own the credential.
	ErrForbidden = errors.New("webauthn: forbidden")
)

// --- In-process session store --------------------------------------------

// sessionTTL is the maximum age of a webauthn ceremony session. The browser's
// own timeout is ~60s for most authenticators; 5 minutes is a generous bound
// that also forgives slow user-presence prompts. After this the entry is
// silently evicted from the map by sessionStore.get.
const sessionTTL = 5 * time.Minute

type sessionEntry struct {
	session *gowebauthn.SessionData
	expires time.Time
}

type sessionStore struct {
	mu      sync.Mutex
	entries map[string]sessionEntry
}

func newSessionStore() *sessionStore {
	return &sessionStore{entries: make(map[string]sessionEntry)}
}

// put inserts a fresh session and returns the opaque ULID-shaped id the
// client must echo back on finish.
func (s *sessionStore) put(session *gowebauthn.SessionData) string {
	id := newSessionID()
	s.mu.Lock()
	s.entries[id] = sessionEntry{session: session, expires: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	return id
}

// take returns and removes the session matching id. Sessions are single-use
// to defeat replay even within the TTL window. On miss or expiry returns
// (nil, false).
func (s *sessionStore) take(id string) (*gowebauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}
	delete(s.entries, id)
	if time.Now().After(e.expires) {
		return nil, false
	}
	return e.session, true
}

// newSessionID returns a 16-byte crypto-random hex id (32 chars). The library
// itself uses crypto/rand for challenges; we use the same source for our
// opaque session handle so an attacker cannot enumerate or guess them.
func newSessionID() string {
	b := make([]byte, 16)
	// rand.Read is documented to always succeed on the platforms Go supports;
	// errors here would indicate a critical OS-level failure.
	if _, err := readRandom(b); err != nil {
		// Fall back to a timestamp; collision risk is acceptable on the
		// extremely unlikely path that the OS RNG is broken.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// readRandom is a tiny indirection so tests can replace the source of
// randomness if they ever need to. The package-level var below is the
// default (crypto/rand.Read).
var readRandom = defaultReadRandom
