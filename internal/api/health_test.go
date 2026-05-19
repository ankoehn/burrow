package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func TestHealthz(t *testing.T) {
	d := Deps{Log: discardLog()}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	r, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("healthz status=%d", r.StatusCode)
	}
	_ = r.Body.Close()
}

func TestReadyz(t *testing.T) {
	// DB healthy -> 200
	d := Deps{Log: discardLog(), DB: fakePinger{}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	r, _ := http.Get(srv.URL + "/readyz")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("readyz healthy status=%d", r.StatusCode)
	}
	_ = r.Body.Close()

	// DB down -> 503
	d2 := Deps{Log: discardLog(), DB: fakePinger{err: errors.New("closed")}}
	srv2 := httptest.NewServer(NewRouter(d2))
	defer srv2.Close()
	r2, _ := http.Get(srv2.URL + "/readyz")
	if r2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz down status=%d want 503", r2.StatusCode)
	}
	_ = r2.Body.Close()

	// No pinger configured -> still 503 (not ready)
	srv3 := httptest.NewServer(NewRouter(Deps{Log: discardLog()}))
	defer srv3.Close()
	r3, _ := http.Get(srv3.URL + "/readyz")
	if r3.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz no-pinger status=%d want 503", r3.StatusCode)
	}
	_ = r3.Body.Close()
}
