package db

import (
	"context"
	"errors"
	"testing"
)

func TestWebAuthnCredentialCRUD(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}

	aaguid := "00000000-0000-0000-0000-000000000000"
	transports := "usb,nfc"
	cred := WebAuthnCredential{
		ID:         "cred-base64url-abc",
		UserID:     "u1",
		Label:      "yubikey-blue",
		PublicKey:  []byte{0xa5, 0x01, 0x02, 0x03},
		SignCount:  0,
		AAGUID:     &aaguid,
		Transports: &transports,
	}
	if err := x.CreateWebAuthnCredential(ctx, cred); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := x.GetWebAuthnCredential(ctx, "cred-base64url-abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != "u1" || got.Label != "yubikey-blue" || string(got.PublicKey) != string(cred.PublicKey) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.AAGUID == nil || *got.AAGUID != aaguid {
		t.Fatalf("aaguid mismatch: %v", got.AAGUID)
	}
	if got.Transports == nil || *got.Transports != transports {
		t.Fatalf("transports mismatch: %v", got.Transports)
	}
	if got.LastUsed != nil {
		t.Fatalf("last_used must be nil before first use; got %v", got.LastUsed)
	}

	if _, err := x.GetWebAuthnCredential(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get miss: want ErrNotFound got %v", err)
	}

	all, err := x.ListWebAuthnCredentialsByUser(ctx, "u1")
	if err != nil || len(all) != 1 {
		t.Fatalf("list-by-user: %v len=%d", err, len(all))
	}

	// Sign-count increments via update; last_used populates.
	if err := x.UpdateWebAuthnSignCount(ctx, "cred-base64url-abc", 7); err != nil {
		t.Fatalf("update sign_count: %v", err)
	}
	got2, _ := x.GetWebAuthnCredential(ctx, "cred-base64url-abc")
	if got2.SignCount != 7 {
		t.Fatalf("sign_count after update: want 7 got %d", got2.SignCount)
	}
	if got2.LastUsed == nil {
		t.Fatalf("last_used must be populated after UpdateSignCount")
	}

	if err := x.UpdateWebAuthnSignCount(ctx, "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update miss: want ErrNotFound got %v", err)
	}

	if err := x.DeleteWebAuthnCredential(ctx, "cred-base64url-abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := x.GetWebAuthnCredential(ctx, "cred-base64url-abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound got %v", err)
	}
	if err := x.DeleteWebAuthnCredential(ctx, "cred-base64url-abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete: want ErrNotFound got %v", err)
	}
}

func TestWebAuthnCredentialUserCascade(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	cred := WebAuthnCredential{
		ID:        "cred-x",
		UserID:    "u1",
		Label:     "k",
		PublicKey: []byte{0x01},
	}
	if err := x.CreateWebAuthnCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if _, err := x.DB().ExecContext(ctx, `DELETE FROM users WHERE id=?`, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.GetWebAuthnCredential(ctx, "cred-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user delete must cascade webauthn_credentials; got %v", err)
	}
}

func TestWebAuthnCredentialNullableMetadata(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	cred := WebAuthnCredential{
		ID:        "no-meta",
		UserID:    "u1",
		Label:     "minimal",
		PublicKey: []byte{0x42},
		// AAGUID and Transports stay nil.
	}
	if err := x.CreateWebAuthnCredential(ctx, cred); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := x.GetWebAuthnCredential(ctx, "no-meta")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AAGUID != nil {
		t.Fatalf("aaguid must be nil when not set; got %v", *got.AAGUID)
	}
	if got.Transports != nil {
		t.Fatalf("transports must be nil when not set; got %v", *got.Transports)
	}
}
