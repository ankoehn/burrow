package proto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthRequestHostnameReserved(t *testing.T) {
	// omitempty: absent when zero
	b, err := json.Marshal(AuthRequest{Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hostname") {
		t.Fatalf("hostname must be omitted when empty: %s", b)
	}
	// round-trips when set
	var got AuthRequest
	if err := json.Unmarshal([]byte(`{"token":"t","hostname":"box-1"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Hostname != "box-1" {
		t.Fatalf("hostname not decoded: %+v", got)
	}
}
