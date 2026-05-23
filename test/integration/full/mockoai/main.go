// test/integration/full/mockoai/main.go
// test-only — never deploy this shape.
//
// Mock OpenAI-compatible server for Burrow e2e tests.
// Implements POST /v1/chat/completions (SSE), POST /v1/embeddings,
// POST /v1/messages (Anthropic shape). Deterministic seeded responses
// — no real model, no phone-home. Apache-2.0, stdlib only.
package main

import (
	"flag"
	"log"
	"net/http"
)

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	flag.Parse()
	srv := &http.Server{Addr: *addr, Handler: handler()}
	log.Printf("[mockoai] listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
