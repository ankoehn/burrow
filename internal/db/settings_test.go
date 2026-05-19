package db

import (
	"context"
	"testing"
)

func TestSettingsUpsert(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if got, err := x.GetAllSettings(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty settings: %+v %v", got, err)
	}
	if err := x.SetSettings(ctx, map[string]string{"smtp.host": "mail.example.com", "smtp.port": "587"}); err != nil {
		t.Fatal(err)
	}
	// overwrite one, add one
	if err := x.SetSettings(ctx, map[string]string{"smtp.host": "mx.example.com", "smtp.from": "noreply@example.com"}); err != nil {
		t.Fatal(err)
	}
	got, err := x.GetAllSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, s := range got {
		m[s.Key] = s.Value
	}
	if m["smtp.host"] != "mx.example.com" || m["smtp.port"] != "587" || m["smtp.from"] != "noreply@example.com" {
		t.Fatalf("unexpected settings: %+v", m)
	}
}
