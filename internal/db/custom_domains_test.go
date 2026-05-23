package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedCustomDomain inserts a parent user + service + custom-domain row and
// returns the inserted domain row (with its generated ID).
func seedCustomDomain(t *testing.T, x *DB) ServiceCustomDomain {
	t.Helper()
	ctx := context.Background()

	uid := "u-" + uuid.NewString()[:8]
	sid := "s-" + uuid.NewString()[:8]
	subdomain := "scd" + uuid.NewString()[:6]
	if _, err := x.DB().ExecContext(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES(?,?,?,?)`,
		uid, uid+"@test.invalid", "h", "user"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := x.DB().ExecContext(ctx,
		`INSERT INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
		   VALUES(?,?,?,?,?,?,?)`,
		sid, uid, "svc", "http", subdomain, "open", "Authorization"); err != nil {
		t.Fatalf("seed service: %v", err)
	}
	row := ServiceCustomDomain{
		ServiceID:  sid,
		Hostname:   "host-" + uuid.NewString()[:6] + ".example.com",
		CertPEM:    "PEM",
		KeyPEM:     "KEY",
		CertSHA256: "deadbeef",
		NotBefore:  time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:   time.Now().UTC().Add(60 * 24 * time.Hour),
	}
	inserted, err := x.InsertCustomDomain(ctx, row)
	if err != nil {
		t.Fatalf("InsertCustomDomain: %v", err)
	}
	return inserted
}

// TestInsertCustomDomainDefaultStatus verifies the v0.5.2 schema default —
// rows without an explicit status get "active" and a current
// status_updated_at.
func TestInsertCustomDomainDefaultStatus(t *testing.T) {
	x := testDB(t)
	d := seedCustomDomain(t, x)

	got, err := x.GetCustomDomain(context.Background(), d.ServiceID, d.ID)
	if err != nil {
		t.Fatalf("GetCustomDomain: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("default Status = %q; want %q", got.Status, "active")
	}
	if got.StatusUpdatedAt.IsZero() {
		t.Error("StatusUpdatedAt is zero; want a current timestamp")
	}
}

// TestUpdateCustomDomainStatus persists a transition and bumps
// status_updated_at; subsequent reads via GetCustomDomain / ListCustomDomains
// / ListAllCustomDomains / LookupCustomDomainByHostname all see the new
// status.
func TestUpdateCustomDomainStatus(t *testing.T) {
	x := testDB(t)
	d := seedCustomDomain(t, x)
	ctx := context.Background()

	if err := x.UpdateCustomDomainStatus(ctx, d.ID, "cert_expiring"); err != nil {
		t.Fatalf("UpdateCustomDomainStatus: %v", err)
	}

	got, err := x.GetCustomDomain(ctx, d.ServiceID, d.ID)
	if err != nil {
		t.Fatalf("GetCustomDomain: %v", err)
	}
	if got.Status != "cert_expiring" {
		t.Errorf("Status after update = %q; want %q", got.Status, "cert_expiring")
	}
	if !got.StatusUpdatedAt.After(d.StatusUpdatedAt) && !got.StatusUpdatedAt.Equal(d.StatusUpdatedAt) {
		// On fast Windows clocks the timestamps can be identical; we only
		// require monotonic non-decrease.
		t.Errorf("StatusUpdatedAt did not advance: before=%v after=%v",
			d.StatusUpdatedAt, got.StatusUpdatedAt)
	}

	list, err := x.ListCustomDomains(ctx, d.ServiceID)
	if err != nil {
		t.Fatalf("ListCustomDomains: %v", err)
	}
	if len(list) != 1 || list[0].Status != "cert_expiring" {
		t.Errorf("ListCustomDomains status = %v; want one row with cert_expiring", list)
	}

	all, err := x.ListAllCustomDomains(ctx)
	if err != nil {
		t.Fatalf("ListAllCustomDomains: %v", err)
	}
	if len(all) != 1 || all[0].Status != "cert_expiring" {
		t.Errorf("ListAllCustomDomains status = %v; want one row with cert_expiring", all)
	}

	byHost, err := x.LookupCustomDomainByHostname(ctx, d.Hostname)
	if err != nil {
		t.Fatalf("LookupCustomDomainByHostname: %v", err)
	}
	if byHost.Status != "cert_expiring" {
		t.Errorf("LookupCustomDomainByHostname status = %q; want %q",
			byHost.Status, "cert_expiring")
	}
}

// TestUpdateCustomDomainStatusUnknownID returns ErrNotFound when no row
// matches.
func TestUpdateCustomDomainStatusUnknownID(t *testing.T) {
	x := testDB(t)
	err := x.UpdateCustomDomainStatus(context.Background(), "no-such-id", "cert_expired")
	if err == nil {
		t.Error("UpdateCustomDomainStatus with unknown id = nil; want error")
	}
}
