package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/events"
)

type fakeLister struct{ v []TunnelView }

func (f fakeLister) ListUserTunnels(string) []TunnelView { return f.v }

func TestListTunnels(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	d := Deps{Users: u, Log: discardLog(),
		Tunnels: fakeLister{v: []TunnelView{{ID: "t1", Name: "web", Type: "tcp", RemotePort: 9000, LocalAddr: "127.0.0.1:3000", BytesIn: 11, BytesOut: 22, Connected: true}}}}
	ts := newTestServer(d)
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})
	r, _ := cl.Get(ts.URL + "/api/v1/tunnels")
	b := readBody(t, r)
	if r.StatusCode != http.StatusOK ||
		!strings.Contains(b, `"id":"t1"`) || !strings.Contains(b, `"bytes_in":11`) ||
		!strings.Contains(b, `"bytes_out":22`) || !strings.Contains(b, `"connected":true`) {
		t.Fatalf("tunnels body=%s status=%d", b, r.StatusCode)
	}
}

func TestSSEReceivesEventThenDisconnects(t *testing.T) {
	bus := events.NewBus()
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog(), Events: bus})
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	// Give the handler a beat to Subscribe, then publish.
	time.Sleep(100 * time.Millisecond)
	bus.PublishTunnelsChanged("u1")
	sc := bufio.NewScanner(resp.Body)
	got := make(chan string, 1)
	go func() {
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: tunnels") {
				got <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive SSE tunnels event")
	}
	cancel() // client disconnects
	resp.Body.Close()
	// handler must return promptly (no leaked goroutine): give it a moment, then
	// publishing again must not panic and the bus must drop the subscriber.
	time.Sleep(200 * time.Millisecond)
	bus.PublishTunnelsChanged("u1")
	if n := bus.SubscriberCountForTest("u1"); n != 0 {
		t.Fatalf("SSE handler leaked a subscriber: %d", n)
	}
}

func TestEventsStreamNilEventsIs500(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()}) // Events nil
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})
	r, err := cl.Get(ts.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("nil Events want 500, got %d", r.StatusCode)
	}
}

func TestListTunnelsNilListerIsEmptyArray(t *testing.T) {
	u := &tokUsers{}
	u.verify = func(_, _ string) (bool, error) { return true, nil }
	ts := newTestServer(Deps{Users: u, Log: discardLog()}) // Tunnels nil
	defer ts.Close()
	cl := loggedInClient(t, &httptestServer{ts})
	r, _ := cl.Get(ts.URL + "/api/v1/tunnels")
	b := strings.TrimSpace(readBody(t, r))
	if r.StatusCode != http.StatusOK || b != "[]" {
		t.Fatalf("nil Tunnels must yield 200 [] (status=%d body=%q)", r.StatusCode, b)
	}
}
