package aimeter_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/aimeter"
	"github.com/ankoehn/burrow/internal/db"
)

// TestAnthropicStreamUsageAccumulates — scenario 3 of the plan: feed
// message_start + 3 content_block_delta + message_delta (with usage) +
// message_stop and assert the accumulator returns the Anthropic-shaped
// tokens.
func TestAnthropicStreamUsageAccumulates(t *testing.T) {
	raw := mustReadFixture(t, "anthropic_stream.sse")
	frames := splitSSEFrames(t, raw)
	if len(frames) < 5 {
		t.Fatalf("fixture should have >=5 frames, got %d", len(frames))
	}

	visitor := newRecordingWriter()
	s := aimeter.WrapResponse(visitor, aimeter.KindAnthropic)

	reader := &frameDripReader{frames: frames, gap: 15 * time.Millisecond}
	if _, err := io.Copy(s, reader); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := s.Tokens()
	if got.In != 12 || got.Out != 7 {
		t.Fatalf("tokens: got %+v want In=12 Out=7", got)
	}

	if !visitor.NonBuffered(10*time.Millisecond, len(frames)-1) {
		t.Fatal("buffered Anthropic stream — SSE invariant violated (not enough wide gaps between writes)")
	}
}

// TestParseAnthropicBody — non-stream Anthropic /v1/messages response.
func TestParseAnthropicBody(t *testing.T) {
	body := []byte(`{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":12,"output_tokens":7}}`)
	got := aimeter.ParseAnthropicBody(body)
	want := aimeter.Tokens{In: 12, Out: 7, Total: 19}
	if got != want {
		t.Fatalf("ParseAnthropicBody: got %+v want %+v", got, want)
	}
}

// TestStreamPassthroughKind — Kind values without a dedicated parser
// (e.g. MCP, unknown) still forward bytes verbatim and flush.
func TestStreamPassthroughKind(t *testing.T) {
	visitor := newRecordingWriter()
	s := aimeter.WrapResponse(visitor, aimeter.KindMCP)
	payload := []byte("arbitrary opaque bytes\n")
	n, err := s.Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Fatalf("short write: n=%d want %d", n, len(payload))
	}
	if string(visitor.Bytes()) != string(payload) {
		t.Fatalf("passthrough body differs: %q vs %q", visitor.Bytes(), payload)
	}
	if len(visitor.flushes) == 0 {
		t.Fatal("passthrough did not flush")
	}
}

// TestStreamKindAccessor confirms the Kind diagnostic accessor.
func TestStreamKindAccessor(t *testing.T) {
	s := aimeter.WrapResponse(newRecordingWriter(), aimeter.KindOpenAI)
	if s.Kind() != aimeter.KindOpenAI {
		t.Fatalf("Kind: got %q want %q", s.Kind(), aimeter.KindOpenAI)
	}
}

// --- SQLSink ----------------------------------------------------------------

// testDB opens an isolated, fully-migrated sqlite database in a temp dir.
// We bootstrap our own copy here (mirroring the db package's internal
// testDB) rather than importing the db package's unexported helper.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	x := db.Wrap(d)
	t.Cleanup(func() { _ = x.Close() })
	return x
}

// seedService inserts a row into services so the FK on usage_events is
// satisfied. Returns the service id.
func seedService(t *testing.T, x *db.DB) string {
	t.Helper()
	ctx := context.Background()
	// Need a user first (services.user_id FK).
	if err := x.CreateUser(ctx, db.User{ID: "u-sink", Email: "sink@test", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc, err := x.GetOrCreateService(ctx, "u-sink", "openai-svc", "http")
	if err != nil {
		t.Fatalf("seed service: %v", err)
	}
	return svc.ID
}

func TestSQLSinkRecord(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	serviceID := seedService(t, x)

	sink := aimeter.NewSQLSink(x)
	sample := aimeter.Sample{
		ServiceID:      serviceID,
		APIKeyID:       "", // optional
		Model:          "gpt-4o-mini",
		Kind:           aimeter.KindOpenAI,
		TokensIn:       12,
		TokensOut:      7,
		BytesIn:        1024,
		BytesOut:       4096,
		Streamed:       true,
		CacheHit:       false,
		UpstreamStatus: 200,
	}
	if err := sink.Record(ctx, sample); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Read the row back via raw SQL — the db package doesn't expose a
	// typed read helper for usage_events yet (Task 21 owns the query layer).
	var (
		id, svcID, apiKeyID, kind string
		tokensIn, tokensOut       int64
		bytesIn, bytesOut         int64
		streamed, cacheHit        int
		upstreamStatus            int
	)
	err := x.DB().QueryRowContext(ctx, `
		SELECT id, service_id, api_key_id, kind,
		       tokens_in, tokens_out, bytes_in, bytes_out,
		       streamed, cache_hit, upstream_status
		  FROM usage_events
		 WHERE service_id=?`, serviceID).Scan(
		&id, &svcID, &apiKeyID, &kind,
		&tokensIn, &tokensOut, &bytesIn, &bytesOut,
		&streamed, &cacheHit, &upstreamStatus)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if id == "" {
		t.Fatal("id should be populated")
	}
	if svcID != serviceID {
		t.Fatalf("service_id: got %q want %q", svcID, serviceID)
	}
	if apiKeyID != "" {
		t.Fatalf("api_key_id: got %q want empty", apiKeyID)
	}
	if kind != "openai" {
		t.Fatalf("kind: got %q want openai", kind)
	}
	if tokensIn != 12 || tokensOut != 7 {
		t.Fatalf("tokens: got in=%d out=%d want 12/7", tokensIn, tokensOut)
	}
	if bytesIn != 1024 || bytesOut != 4096 {
		t.Fatalf("bytes: got in=%d out=%d want 1024/4096", bytesIn, bytesOut)
	}
	if streamed != 1 || cacheHit != 0 {
		t.Fatalf("bool flags: streamed=%d cache_hit=%d want 1/0", streamed, cacheHit)
	}
	if upstreamStatus != 200 {
		t.Fatalf("upstream_status: got %d want 200", upstreamStatus)
	}
}

// TestSQLSinkRecord_NilSafeguards — Record on a nil sink or nil DB is a
// no-op (used so call sites can safely skip metering without a guard).
func TestSQLSinkRecord_NilSafeguards(t *testing.T) {
	ctx := context.Background()
	var s *aimeter.SQLSink
	if err := s.Record(ctx, aimeter.Sample{}); err != nil {
		t.Fatalf("nil sink Record: %v", err)
	}
	s = &aimeter.SQLSink{DB: nil}
	if err := s.Record(ctx, aimeter.Sample{}); err != nil {
		t.Fatalf("nil DB Record: %v", err)
	}
}
