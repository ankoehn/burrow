package credinject_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/ankoehn/burrow/internal/credinject"
)

// stubVault is an in-test Vault backed by a plain map.
type stubVault struct{ m map[string]string }

func (s stubVault) Get(slot string) (string, bool) { v, ok := s.m[slot]; return v, ok }
func (s stubVault) Slots() []string                 { return nil }

// stubStore is an in-test Store that returns a fixed Binding (or none).
type stubStore struct {
	bind   credinject.Binding
	bound  bool // true → bind is valid; false → unbound
	putErr error
	delErr error
	// for inspection
	lastPut credinject.Binding
	lastDel string
}

func (s *stubStore) GetBinding(_ context.Context, _ string) (credinject.Binding, bool, error) {
	return s.bind, s.bound, nil
}
func (s *stubStore) PutBinding(_ context.Context, b credinject.Binding) error {
	s.lastPut = b
	return s.putErr
}
func (s *stubStore) DeleteBinding(_ context.Context, serviceID string) error {
	s.lastDel = serviceID
	return s.delErr
}

func TestInjectorStripsVisitorAuthAndAppliesHeaderFormat(t *testing.T) {
	v := stubVault{map[string]string{"OPENAI": "sk-real"}}
	s := &stubStore{
		bind: credinject.Binding{
			ServiceID:    "svc1",
			Slot:         "OPENAI",
			HeaderName:   "Authorization",
			HeaderFormat: "Bearer {key}",
		},
		bound: true,
	}
	i := credinject.New(v, s, slog.Default())
	r, _ := http.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer visitor-burrow-key")
	ok, err := i.Apply(context.Background(), "svc1", r)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer sk-real" {
		t.Errorf("got %q; want Bearer sk-real", got)
	}
}

func TestInjectorUnboundPassThrough(t *testing.T) {
	v := stubVault{map[string]string{"OPENAI": "sk-real"}}
	s := &stubStore{bound: false}
	i := credinject.New(v, s, slog.Default())
	r, _ := http.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer visitor-key")
	ok, err := i.Apply(context.Background(), "svc-none", r)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("unbound service should return false")
	}
	// Header must be untouched.
	if got := r.Header.Get("Authorization"); got != "Bearer visitor-key" {
		t.Errorf("pass-through should leave header unchanged; got %q", got)
	}
}

func TestInjectorMissingSlotEnvFiresMissCounter(t *testing.T) {
	// Vault has no key for OPENAI.
	v := stubVault{map[string]string{}}
	s := &stubStore{
		bind: credinject.Binding{
			ServiceID:    "svc2",
			Slot:         "OPENAI",
			HeaderName:   "Authorization",
			HeaderFormat: "Bearer {key}",
		},
		bound: true,
	}
	var missFired bool
	i := credinject.New(v, s, slog.Default())
	i.OnMiss = func(serviceID string) { missFired = true }

	r, _ := http.NewRequest("POST", "/v1/chat", nil)
	ok, err := i.Apply(context.Background(), "svc2", r)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("missing slot should return false (pass-through)")
	}
	if !missFired {
		t.Error("OnMiss hook should have been called")
	}
}

func TestInjectorHeaderFormatCustomTemplate(t *testing.T) {
	v := stubVault{map[string]string{"ANTHROPIC": "sk-ant-1"}}
	s := &stubStore{
		bind: credinject.Binding{
			ServiceID:    "svc3",
			Slot:         "ANTHROPIC",
			HeaderName:   "x-api-key",
			HeaderFormat: "{key}",
		},
		bound: true,
	}
	i := credinject.New(v, s, slog.Default())
	r, _ := http.NewRequest("POST", "/v1/messages", nil)
	ok, err := i.Apply(context.Background(), "svc3", r)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if got := r.Header.Get("x-api-key"); got != "sk-ant-1" {
		t.Errorf("got %q; want sk-ant-1", got)
	}
}

func TestInjectorOnlyFirstKeyPlaceholderReplaced(t *testing.T) {
	// Format has {key} twice; only the first should be replaced.
	v := stubVault{map[string]string{"SLOT": "myval"}}
	s := &stubStore{
		bind: credinject.Binding{
			ServiceID:    "svc4",
			Slot:         "SLOT",
			HeaderName:   "X-Custom",
			HeaderFormat: "{key}+{key}",
		},
		bound: true,
	}
	i := credinject.New(v, s, slog.Default())
	r, _ := http.NewRequest("GET", "/", nil)
	ok, err := i.Apply(context.Background(), "svc4", r)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if got := r.Header.Get("X-Custom"); got != "myval+{key}" {
		t.Errorf("got %q; want myval+{key} (only first occurrence replaced)", got)
	}
}
