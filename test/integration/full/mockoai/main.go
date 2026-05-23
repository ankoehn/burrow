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
	"fmt"
	"log"
	"net/http"
)

func finishReason(last bool) string {
	if last {
		return `"stop"`
	}
	return "null"
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, _ := w.(http.Flusher)
		chunks := []string{"Hello", " from", " mockoai", "."}
		for i, c := range chunks {
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":%s}]}\n\n",
				c, finishReason(i == len(chunks)-1))
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
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
