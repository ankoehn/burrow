package store

// automation.go — automation-token mint/list/revoke (spec Part M.1).
// Plaintext bearer tokens are emitted exactly once at mint time. The store
// persists only the sha256-hex hash of the plaintext; lookup is by hash.
// Mint validates the requested permission set is a subset of the caller's
// CURRENT role's permission set (no privilege escalation possible — even by
// the minter themselves).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/db"
)

// AutomationTokenPrefix is the secret-scanning prefix of every bearer token.
const AutomationTokenPrefix = "bua_"

// AutomationTokenPrefixLen is the number of characters of the plaintext that
// are stored verbatim in the prefix column (for UI list previews — the rest
// of the secret is never persisted). Includes the "bua_" prefix.
const AutomationTokenPrefixLen = 10

// ErrPermissionNotInRole is returned by MintAutomationToken when the request
// asks for a permission the caller's current role does NOT hold. The handler
// maps this to HTTP 403.
var ErrPermissionNotInRole = errors.New("store: requested permission not granted by caller role")

// ErrTokenNameRequired is returned by MintAutomationToken on an empty name.
// Mapped to HTTP 400.
var ErrTokenNameRequired = errors.New("store: token name is required")

// AutomationTokenView is the wire-friendly view of an automation_tokens row.
// The plaintext is returned exactly once at mint time (see MintAutomationToken).
type AutomationTokenView struct {
	ID          string
	Name        string
	Prefix      string
	UserID      string
	RoleAtMint  string
	Permissions []string
	ExpiresAt   *time.Time
	LastUsed    *time.Time
	CreatedAt   time.Time
}

// MintAutomationToken issues a new bearer token for the caller. The
// permissions argument MUST be a subset of the caller's current role's
// permission set (otherwise ErrPermissionNotInRole is returned BEFORE any
// DB write). The plaintext "bua_<base32>" is returned exactly once.
//
// Storage: sha256-hex(plaintext) only. The 4-char "bua_" prefix plus the
// next 6 characters of the base32 body are persisted as `prefix` to support
// list-screen previews.
func (s *Store) MintAutomationToken(
	ctx context.Context,
	callerID, callerRole, name string,
	permissions []string,
	expiresAt *time.Time,
) (AutomationTokenView, string, error) {
	if strings.TrimSpace(name) == "" {
		return AutomationTokenView{}, "", ErrTokenNameRequired
	}
	// Subset check: every requested permission must be one the current
	// caller role grants. Admin trivially passes (admin has all). Unknown
	// permission keys are rejected as not-in-role (no need to also validate
	// against authz.AllPermissions — anything not in role fails here).
	for _, p := range permissions {
		if !authz.Can(callerRole, authz.Permission(p)) {
			return AutomationTokenView{}, "", ErrPermissionNotInRole
		}
	}

	plaintext, hash, err := generateBearerToken()
	if err != nil {
		return AutomationTokenView{}, "", fmt.Errorf("generate bearer: %w", err)
	}

	prefix := plaintext
	if len(prefix) > AutomationTokenPrefixLen {
		prefix = prefix[:AutomationTokenPrefixLen]
	}

	permsJSON, err := json.Marshal(permissions)
	if err != nil {
		return AutomationTokenView{}, "", fmt.Errorf("marshal permissions: %w", err)
	}

	var expCopy *time.Time
	if expiresAt != nil {
		ts := expiresAt.UTC()
		expCopy = &ts
	}

	row := db.AutomationToken{
		ID:          uuid.NewString(),
		Name:        name,
		Prefix:      prefix,
		UserID:      callerID,
		RoleAtMint:  callerRole,
		TokenHash:   hash,
		Permissions: string(permsJSON),
		ExpiresAt:   expCopy,
	}
	if err := s.q.CreateAutomationToken(ctx, row); err != nil {
		return AutomationTokenView{}, "", err
	}

	// Re-read to pick up the SQLite-assigned created_at default.
	stored, err := s.q.GetAutomationToken(ctx, row.ID)
	if err != nil {
		return AutomationTokenView{}, "", err
	}
	return toAutomationTokenView(stored), plaintext, nil
}

// ListAutomationTokensForCaller returns the tokens visible to the caller.
// :any callers (admin or holders of automation:tokens:manage:any) see every
// row; :own callers see only their own tokens. A caller with neither
// permission gets ErrForbidden.
func (s *Store) ListAutomationTokensForCaller(
	ctx context.Context,
	callerID, callerRole string,
) ([]AutomationTokenView, error) {
	canAny := callerRole == "admin" || authz.Can(callerRole, authz.PermAutomationTokensManageAny)
	canOwn := canAny || authz.Can(callerRole, authz.PermAutomationTokensManageOwn)
	if !canOwn {
		return nil, ErrForbidden
	}
	var rows []db.AutomationToken
	var err error
	if canAny {
		rows, err = s.q.ListAutomationTokens(ctx)
	} else {
		rows, err = s.q.ListAutomationTokensByUser(ctx, callerID)
	}
	if err != nil {
		return nil, err
	}
	out := make([]AutomationTokenView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAutomationTokenView(r))
	}
	return out, nil
}

// RevokeAutomationToken deletes the token with the given id. :any callers
// may revoke any token; :own callers may only revoke their own (a foreign
// token surfaces as db.ErrNotFound to keep ownership opaque). A caller with
// neither permission gets ErrForbidden.
func (s *Store) RevokeAutomationToken(
	ctx context.Context,
	callerID, callerRole, id string,
) error {
	canAny := callerRole == "admin" || authz.Can(callerRole, authz.PermAutomationTokensManageAny)
	canOwn := canAny || authz.Can(callerRole, authz.PermAutomationTokensManageOwn)
	if !canOwn {
		return ErrForbidden
	}
	tok, err := s.q.GetAutomationToken(ctx, id)
	if err != nil {
		return err
	}
	if !canAny && tok.UserID != callerID {
		// Hide ownership: surface as NotFound (matches existing service-CRUD
		// scoping pattern).
		return db.ErrNotFound
	}
	return s.q.DeleteAutomationToken(ctx, id)
}

// LookupBearer is the surface consumed by RequireBearerOrSession: it hashes
// the plaintext and returns the row, or db.ErrNotFound. The middleware then
// inspects ExpiresAt, ctx-injects the user id + token id + permission set,
// and (best-effort) calls TouchBearer.
func (s *Store) LookupBearer(ctx context.Context, hash string) (AutomationTokenView, error) {
	row, err := s.q.GetAutomationTokenByHash(ctx, hash)
	if err != nil {
		return AutomationTokenView{}, err
	}
	return toAutomationTokenView(row), nil
}

// TouchBearer updates last_used. Best-effort; the bearer middleware logs but
// does not fail the request when this errors.
func (s *Store) TouchBearer(ctx context.Context, id string) error {
	return s.q.TouchAutomationTokenLastUsed(ctx, id)
}

// generateBearerToken returns plaintext + sha256-hex hash. The plaintext is
// "bua_" + base32(crockford-RawNoPadding)(32 random bytes), giving a high-
// entropy URL-safe, double-click-selectable secret.
func generateBearerToken() (plaintext, hashed string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	enc := strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "=")
	plaintext = AutomationTokenPrefix + enc
	sum := sha256.Sum256([]byte(plaintext))
	hashed = hex.EncodeToString(sum[:])
	return plaintext, hashed, nil
}

func toAutomationTokenView(r db.AutomationToken) AutomationTokenView {
	var perms []string
	if strings.TrimSpace(r.Permissions) != "" {
		_ = json.Unmarshal([]byte(r.Permissions), &perms)
	}
	if perms == nil {
		perms = []string{}
	}
	v := AutomationTokenView{
		ID:          r.ID,
		Name:        r.Name,
		Prefix:      r.Prefix,
		UserID:      r.UserID,
		RoleAtMint:  r.RoleAtMint,
		Permissions: perms,
		ExpiresAt:   r.ExpiresAt,
		LastUsed:    r.LastUsed,
		CreatedAt:   r.CreatedAt,
	}
	return v
}
