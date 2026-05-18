package api

import (
	"net/http"
	"testing"
)

func spaSpy() (http.Handler, *bool) {
	hit := false
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<div id=\"root\"></div>"))
	}), &hit
}

func TestSPAMountedServesNonAPIAndNotAPI(t *testing.T) {
	spa, hit := spaSpy()
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog(), SPA: spa})
	defer ts.Close()

	r, _ := http.Get(ts.URL + "/")
	r.Body.Close()
	if r.StatusCode != 200 || !*hit {
		t.Fatalf("/ should hit SPA: status=%d hit=%v", r.StatusCode, *hit)
	}
	*hit = false
	r2, _ := http.Get(ts.URL + "/some/client/route")
	r2.Body.Close()
	if r2.StatusCode != 200 || !*hit {
		t.Fatalf("client route should hit SPA: status=%d hit=%v", r2.StatusCode, *hit)
	}
	*hit = false
	r3, _ := http.Get(ts.URL + "/api/v1/nope")
	r3.Body.Close()
	if *hit {
		t.Fatal("/api/v1/* must never fall through to the SPA")
	}
	*hit = false
	r4, _ := http.Get(ts.URL + "/api/v1/tunnels")
	r4.Body.Close()
	if r4.StatusCode != http.StatusUnauthorized || *hit {
		t.Fatalf("/api/v1/tunnels unauth want 401 JSON not SPA: status=%d hit=%v", r4.StatusCode, *hit)
	}

	// The /api/v1 namespace must NEVER fall through to the SPA — not the bare
	// prefix, not a trailing slash, not a wrong method. (The plan's original
	// r.NotFound + /* form leaked exactly these into the SPA.)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1"},
		{"GET", "/api/v1/"},
		{"PUT", "/api/v1/me"},
	} {
		*hit = false
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		rx, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		rx.Body.Close()
		if *hit {
			t.Fatalf("%s %s leaked into the SPA (API namespace must never serve the SPA)", tc.method, tc.path)
		}
	}
}

func TestNoSPAKeeps4bBehavior(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()}) // SPA nil
	defer ts.Close()
	r, _ := http.Get(ts.URL + "/")
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("with SPA nil, / must be chi 404 (4b behavior), got %d", r.StatusCode)
	}
}
