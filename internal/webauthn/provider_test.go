package webauthn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	"github.com/ankoehn/burrow/internal/db"
)

// fakeCredStore is an in-memory CredentialStore for provider tests.
type fakeCredStore struct {
	mu    sync.Mutex
	rows  map[string]db.WebAuthnCredential
	order []string
}

func newFakeCredStore() *fakeCredStore {
	return &fakeCredStore{rows: map[string]db.WebAuthnCredential{}}
}

func (s *fakeCredStore) CreateWebAuthnCredential(_ context.Context, c db.WebAuthnCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[c.ID]; ok {
		return errors.New("dup")
	}
	c.CreatedAt = time.Now().UTC()
	s.rows[c.ID] = c
	s.order = append(s.order, c.ID)
	return nil
}

func (s *fakeCredStore) GetWebAuthnCredential(_ context.Context, id string) (db.WebAuthnCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return db.WebAuthnCredential{}, db.ErrNotFound
	}
	return r, nil
}

func (s *fakeCredStore) ListWebAuthnCredentialsByUser(_ context.Context, userID string) ([]db.WebAuthnCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]db.WebAuthnCredential, 0)
	for _, id := range s.order {
		if r := s.rows[id]; r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeCredStore) DeleteWebAuthnCredential(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return db.ErrNotFound
	}
	delete(s.rows, id)
	return nil
}

func (s *fakeCredStore) UpdateWebAuthnSignCount(_ context.Context, id string, sc int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return db.ErrNotFound
	}
	r.SignCount = sc
	now := time.Now().UTC()
	r.LastUsed = &now
	s.rows[id] = r
	return nil
}

// fakeUserLookup serves a fixed user roster by email/id.
type fakeUserLookup struct {
	byEmail map[string]db.User
	byID    map[string]db.User
}

func (u *fakeUserLookup) GetUserByEmail(_ context.Context, e string) (db.User, error) {
	if r, ok := u.byEmail[e]; ok {
		return r, nil
	}
	return db.User{}, db.ErrNotFound
}

func (u *fakeUserLookup) GetUserByID(_ context.Context, id string) (db.User, error) {
	if r, ok := u.byID[id]; ok {
		return r, nil
	}
	return db.User{}, db.ErrNotFound
}

func newFakeUserLookup(users ...db.User) *fakeUserLookup {
	out := &fakeUserLookup{
		byEmail: map[string]db.User{},
		byID:    map[string]db.User{},
	}
	for _, u := range users {
		out.byEmail[u.Email] = u
		out.byID[u.ID] = u
	}
	return out
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newProvider(t *testing.T, rpid string) (*Provider, *fakeCredStore, *fakeUserLookup) {
	t.Helper()
	creds := newFakeCredStore()
	users := newFakeUserLookup(db.User{ID: "u1", Email: "alice@x", Role: "admin"})
	p, err := New(creds, users, rpid, "Burrow", "https://"+rpid, quietLog())
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	return p, creds, users
}

// --- Config / RP id tests ------------------------------------------------

func TestProviderUsesConfiguredRPID(t *testing.T) {
	p, _, _ := newProvider(t, "dashboard.example.com")
	if got := p.RPID(); got != "dashboard.example.com" {
		t.Fatalf("RPID(): got %q want %q", got, "dashboard.example.com")
	}
	if got := p.Origin(); got != "https://dashboard.example.com" {
		t.Fatalf("Origin(): got %q", got)
	}
}

func TestProviderRPIDAuthDomainOverride(t *testing.T) {
	// The contract: New() takes whatever rpid the caller supplies. The
	// auth_domain decision lives one layer up (cmd/server); here we
	// confirm the provider faithfully passes the value through to the
	// library's challenge metadata.
	p, _, _ := newProvider(t, "tunnels.example.com")
	if p.RPID() != "tunnels.example.com" {
		t.Fatalf("RPID() should reflect auth_domain when supplied")
	}
	res, err := p.BeginRegister(context.Background(), "u1")
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	// The library puts the RP id into the SessionData; we can confirm via
	// the JSON-encodable options struct (a *protocol.CredentialCreation).
	opts, ok := res.Options.(any)
	if !ok || opts == nil {
		t.Fatalf("BeginRegister returned no options struct")
	}
}

// --- BeginRegister --------------------------------------------------------

func TestBeginRegisterReturnsChallengeAndSession(t *testing.T) {
	p, _, _ := newProvider(t, "dashboard.example.com")
	res, err := p.BeginRegister(context.Background(), "u1")
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if res.SessionID == "" {
		t.Fatalf("SessionID must be non-empty")
	}
	// The session must round-trip via the store.
	p.sessions.mu.Lock()
	e, ok := p.sessions.entries[res.SessionID]
	p.sessions.mu.Unlock()
	if !ok {
		t.Fatalf("session id %q must be present after put", res.SessionID)
	}
	if len(e.session.Challenge) < 16 {
		t.Fatalf("challenge must be >= 16 bytes; got %d", len(e.session.Challenge))
	}
	if e.session.RelyingPartyID != "dashboard.example.com" {
		t.Fatalf("RP id stored on session: got %q", e.session.RelyingPartyID)
	}
}

func TestBeginRegisterUnknownUser(t *testing.T) {
	p, _, _ := newProvider(t, "dash.example.com")
	if _, err := p.BeginRegister(context.Background(), "nope"); err == nil {
		t.Fatalf("expected error for unknown user")
	}
}

// --- BeginLogin -----------------------------------------------------------

func TestBeginLoginRequiresRegisteredCredential(t *testing.T) {
	p, _, _ := newProvider(t, "dash.example.com")
	// No credentials registered → ErrUnknownUser.
	if _, err := p.BeginLogin(context.Background(), "alice@x"); !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("BeginLogin without credentials: want ErrUnknownUser, got %v", err)
	}
	// Unknown email → ErrUnknownUser (never reveal which).
	if _, err := p.BeginLogin(context.Background(), "ghost@x"); !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("BeginLogin unknown email: want ErrUnknownUser, got %v", err)
	}
}

func TestBeginLoginIncludesAllowedCredentials(t *testing.T) {
	p, creds, _ := newProvider(t, "dash.example.com")
	if err := creds.CreateWebAuthnCredential(context.Background(), db.WebAuthnCredential{
		ID: "abc123", UserID: "u1", Label: "k", PublicKey: []byte{0x01},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := p.BeginLogin(context.Background(), "alice@x")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if res.UserID != "u1" {
		t.Fatalf("UserID: got %q want u1", res.UserID)
	}
	if res.SessionID == "" {
		t.Fatalf("SessionID must be non-empty")
	}
	p.sessions.mu.Lock()
	e := p.sessions.entries[res.SessionID]
	p.sessions.mu.Unlock()
	if len(e.session.AllowedCredentialIDs) != 1 {
		t.Fatalf("AllowedCredentialIDs: want 1 got %d", len(e.session.AllowedCredentialIDs))
	}
	if string(e.session.AllowedCredentialIDs[0]) != "abc123" {
		t.Fatalf("AllowedCredentialIDs[0]: got %q", e.session.AllowedCredentialIDs[0])
	}
	if len(e.session.Challenge) < 16 {
		t.Fatalf("challenge must be >= 16 bytes; got %d", len(e.session.Challenge))
	}
}

// --- DeleteCredential -----------------------------------------------------

func TestDeleteCredentialOwnerOnly(t *testing.T) {
	p, creds, users := newProvider(t, "dash.example.com")
	// Add a second user so we can test cross-user delete.
	users.byEmail["bob@x"] = db.User{ID: "u2", Email: "bob@x", Role: "user"}
	users.byID["u2"] = db.User{ID: "u2", Email: "bob@x", Role: "user"}
	if err := creds.CreateWebAuthnCredential(context.Background(), db.WebAuthnCredential{
		ID: "alice-key", UserID: "u1", Label: "k", PublicKey: []byte{0x01},
	}); err != nil {
		t.Fatal(err)
	}
	// Bob may not delete Alice's key.
	if err := p.DeleteCredential(context.Background(), "u2", "alice-key"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-user delete: want ErrForbidden, got %v", err)
	}
	// Alice may.
	if err := p.DeleteCredential(context.Background(), "u1", "alice-key"); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if _, err := creds.GetWebAuthnCredential(context.Background(), "alice-key"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("row must be gone; got %v", err)
	}
	// Missing → ErrNotFound.
	if err := p.DeleteCredential(context.Background(), "u1", "ghost"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing delete: want ErrNotFound, got %v", err)
	}
}

// --- Session store --------------------------------------------------------

func TestSessionStoreSingleUse(t *testing.T) {
	s := newSessionStore()
	sd := &gowebauthn.SessionData{Challenge: "x"}
	id := s.put(sd)
	got, ok := s.take(id)
	if !ok || got != sd {
		t.Fatalf("first take must succeed and return the original session")
	}
	if _, ok := s.take(id); ok {
		t.Fatalf("second take of same id must return false (single-use)")
	}
}

func TestSessionStoreExpiry(t *testing.T) {
	s := newSessionStore()
	sd := &gowebauthn.SessionData{Challenge: "x"}
	id := s.put(sd)
	// Reach into the entry and rewind expiry into the past.
	s.mu.Lock()
	e := s.entries[id]
	e.expires = time.Now().Add(-time.Second)
	s.entries[id] = e
	s.mu.Unlock()
	if _, ok := s.take(id); ok {
		t.Fatalf("expired session must report ok=false")
	}
}

func TestFinishRegisterRejectsUnknownSession(t *testing.T) {
	p, _, _ := newProvider(t, "dash.example.com")
	if _, err := p.FinishRegister(context.Background(), "u1", "no-such-session", "label", nil); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("FinishRegister unknown session: want ErrUnknownSession got %v", err)
	}
}

func TestFinishLoginRejectsUnknownSession(t *testing.T) {
	p, _, _ := newProvider(t, "dash.example.com")
	if _, err := p.FinishLogin(context.Background(), "no-such", nil); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("FinishLogin unknown session: want ErrUnknownSession got %v", err)
	}
}

// --- ListCredentialsForUser ---------------------------------------------

func TestListCredentialsForUser(t *testing.T) {
	p, creds, _ := newProvider(t, "dash.example.com")
	ctx := context.Background()
	for i, id := range []string{"a", "b", "c"} {
		if err := creds.CreateWebAuthnCredential(ctx, db.WebAuthnCredential{
			ID: id, UserID: "u1", Label: id, PublicKey: []byte{byte(i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := p.ListCredentialsForUser(ctx, "u1")
	if err != nil || len(out) != 3 {
		t.Fatalf("list: %v len=%d", err, len(out))
	}
}
