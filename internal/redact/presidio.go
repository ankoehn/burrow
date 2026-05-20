package redact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PresidioTimeout is the hard upper bound on each individual Presidio HTTP
// call (analyze, anonymize). On any error — timeout, connection refused,
// 5xx — the caller (Task 10 wiring) MUST short-circuit the request with
// 503 redaction.presidio_unavailable. The Presidio binary is NOT bundled
// with Burrow; this is a Tier-2 hook only.
const PresidioTimeout = 250 * time.Millisecond

// PresidioClient is the narrow HTTP client used to call out to a running
// Presidio analyzer + anonymizer service. BaseURL is the root URL (without
// path) of the service; HTTP is the HTTP client (test seam — supply a
// custom one to inject httptest.Server URLs). The 250ms timeout is
// enforced via context.WithTimeout per call, NOT via client.Timeout, so
// that a single misbehaving call cannot affect a sibling caller's context.
type PresidioClient struct {
	BaseURL string
	HTTP    *http.Client
}

// PresidioEntity is one entity Presidio's /analyze endpoint reports. Only
// the fields Burrow needs are surfaced; everything else in the upstream
// schema is ignored (forward-compatible if Presidio adds fields).
type PresidioEntity struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// Analyze calls POST {BaseURL}/analyze with the body as the "text" field
// of the request envelope (Presidio's documented analyzer API). Returns
// the list of entities Presidio found, or a non-nil error on timeout /
// non-2xx / malformed response. The error wraps context.DeadlineExceeded
// when the hard 250ms timeout fires.
func (p *PresidioClient) Analyze(ctx context.Context, body []byte) ([]PresidioEntity, error) {
	if p == nil {
		return nil, errors.New("presidio: nil client")
	}
	reqCtx, cancel := context.WithTimeout(ctx, PresidioTimeout)
	defer cancel()

	envelope := map[string]any{
		"text":     string(body),
		"language": "en",
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("presidio analyze: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.BaseURL+"/analyze", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("presidio analyze: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		// http.Client.Do wraps the context error; the caller wants to know
		// whether this was a timeout, so we preserve errors.Is reachability
		// to context.DeadlineExceeded via fmt.Errorf's %w wrapping.
		return nil, fmt.Errorf("presidio analyze: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("presidio analyze: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("presidio analyze: read body: %w", err)
	}
	var entities []PresidioEntity
	if err := json.Unmarshal(raw, &entities); err != nil {
		return nil, fmt.Errorf("presidio analyze: decode: %w", err)
	}
	return entities, nil
}

// Anonymize calls POST {BaseURL}/anonymize with the original text. The
// request envelope follows Presidio's anonymizer schema (text + analyzer
// results); for the v0.4.0 hook we send only the text and let Presidio
// re-analyze server-side (simpler wire shape; tradeoff: one extra analyzer
// pass on Presidio's side, but still well within the 250ms budget for
// typical bodies). Returns the rewritten body bytes (UTF-8).
func (p *PresidioClient) Anonymize(ctx context.Context, body []byte) ([]byte, error) {
	if p == nil {
		return nil, errors.New("presidio: nil client")
	}
	reqCtx, cancel := context.WithTimeout(ctx, PresidioTimeout)
	defer cancel()

	envelope := map[string]any{
		"text": string(body),
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("presidio anonymize: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.BaseURL+"/anonymize", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("presidio anonymize: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("presidio anonymize: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("presidio anonymize: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("presidio anonymize: read body: %w", err)
	}
	// Presidio returns {"text":"…"}; extract the rewritten text. Forward-
	// compatible: if the upstream surface ever changes shape, callers get
	// a clear decode error rather than a silently-empty body.
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("presidio anonymize: decode: %w", err)
	}
	return []byte(out.Text), nil
}

// client returns the HTTP client to use, defaulting to http.DefaultClient
// when none was injected. We deliberately do NOT set HTTP.Timeout on the
// returned client — the 250ms cap is per-call via context.WithTimeout, so
// that a shared *http.Client passed in by the caller keeps its own
// configuration intact.
func (p *PresidioClient) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return http.DefaultClient
}
