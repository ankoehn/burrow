package aimeter_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/aimeter"
)

// recordingWriter is an io.Writer + http.Flusher that timestamps every
// Write and Flush call. NonBuffered reports whether successive writes were
// separated by at least gap — the test's signal that the parser is not
// buffering frames.
type recordingWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  []time.Time
	flushes []time.Time
}

func newRecordingWriter() *recordingWriter { return &recordingWriter{} }

func (r *recordingWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, time.Now())
	return r.buf.Write(p)
}

// Flush implements http.Flusher (Stream calls it after each forwarded frame).
func (r *recordingWriter) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes = append(r.flushes, time.Now())
}

// NonBuffered reports whether the writer received at least wantBoundaries
// inter-write gaps of duration >= gap. The intuition: if upstream frames
// are dripped N times with a sleep between each, the visitor writer must
// see at least N-1 wide gaps between writes — proving the parser forwarded
// (and flushed) each frame before the next one arrived. Writes occurring
// within the same upstream batch (e.g. the two physical lines of one SSE
// frame) close together do not invalidate the test.
func (r *recordingWriter) NonBuffered(gap time.Duration, wantBoundaries int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.writes) < 2 {
		return false
	}
	wide := 0
	for i := 1; i < len(r.writes); i++ {
		if r.writes[i].Sub(r.writes[i-1]) >= gap {
			wide++
		}
	}
	return wide >= wantBoundaries
}

// Bytes returns everything forwarded to the writer (test convenience).
func (r *recordingWriter) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf.Bytes()...)
}

// Ensure recordingWriter satisfies http.Flusher so Stream wires up its flush
// path the same way it would for a real http.ResponseWriter.
var _ http.Flusher = (*recordingWriter)(nil)

// frameDripReader emits a fixed set of frames with a sleep between each, to
// simulate a real SSE stream where the upstream sends frames over time.
// io.Copy from this reader into a Stream is the canonical way to assert
// the no-buffering invariant.
type frameDripReader struct {
	frames [][]byte
	gap    time.Duration
	i      int
	pos    int
}

func (f *frameDripReader) Read(p []byte) (int, error) {
	if f.i >= len(f.frames) {
		return 0, io.EOF
	}
	frame := f.frames[f.i]
	if f.pos == 0 && f.i > 0 {
		time.Sleep(f.gap)
	}
	n := copy(p, frame[f.pos:])
	f.pos += n
	if f.pos >= len(frame) {
		f.i++
		f.pos = 0
	}
	return n, nil
}

// splitSSEFrames splits an SSE-formatted byte slice into individual frames
// (each terminated by a blank line "\n\n"). The trailing "\n\n" is kept on
// each returned frame so the writer receives byte-exact bytes.
func splitSSEFrames(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for {
		i := bytes.Index(raw, []byte("\n\n"))
		if i < 0 {
			if len(raw) > 0 {
				out = append(out, raw)
			}
			return out
		}
		end := i + 2
		out = append(out, append([]byte(nil), raw[:end]...))
		raw = raw[end:]
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestParseOpenAIBody — scenario 1 of the plan: non-stream OpenAI response,
// parse body once, return the expected token counts.
func TestParseOpenAIBody(t *testing.T) {
	body := mustReadFixture(t, "openai_non_stream.json")
	got := aimeter.ParseOpenAIBody(body)
	want := aimeter.Tokens{In: 12, Out: 7, Total: 19}
	if got != want {
		t.Fatalf("ParseOpenAIBody: got %+v want %+v", got, want)
	}
}

// TestOpenAIStreamUsageAccumulates — scenario 2 of the plan: stream OpenAI
// response with usage in the final frame; assert tokens correct AND each
// frame was emitted before the next was read.
func TestOpenAIStreamUsageAccumulates(t *testing.T) {
	raw := mustReadFixture(t, "openai_stream_with_usage.sse")
	frames := splitSSEFrames(t, raw)
	if len(frames) < 5 {
		t.Fatalf("fixture should have >=5 frames, got %d", len(frames))
	}

	visitor := newRecordingWriter()
	s := aimeter.WrapResponse(visitor, aimeter.KindOpenAI)

	reader := &frameDripReader{frames: frames, gap: 15 * time.Millisecond}
	if _, err := io.Copy(s, reader); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := s.Tokens()
	if got.In != 12 || got.Out != 7 || got.Total != 19 {
		t.Fatalf("tokens: got %+v want In=12 Out=7 Total=19", got)
	}

	// SSE invariant: with N frames dripped 15ms apart, the visitor must
	// see at least N-1 inter-write gaps of >=10ms — i.e. each frame was
	// forwarded (and flushed) before the next one was read from upstream.
	if !visitor.NonBuffered(10*time.Millisecond, len(frames)-1) {
		t.Fatal("buffered stream — SSE invariant violated (not enough wide gaps between writes)")
	}

	// And: each forwarded frame must have been flushed.
	if len(visitor.flushes) < len(visitor.writes) {
		t.Fatalf("expected at least one Flush per Write; flushes=%d writes=%d",
			len(visitor.flushes), len(visitor.writes))
	}

	// Forwarded bytes must equal the source fixture (byte-exact).
	if !bytes.Equal(visitor.Bytes(), raw) {
		t.Fatalf("forwarded body differs from source: got %d bytes want %d",
			len(visitor.Bytes()), len(raw))
	}
}

// TestOpenAIStreamWithoutUsage_BytesFallback — verifies the byte-estimate
// fallback when no usage object is observed. Per spec Q-mtr, tokens_out
// should be approximately bytes_in/4.
func TestOpenAIStreamWithoutUsage_BytesFallback(t *testing.T) {
	body := []byte(`data: {"choices":[{"delta":{"content":"hello"}}]}` + "\n\n" +
		`data: [DONE]` + "\n\n")
	visitor := newRecordingWriter()
	s := aimeter.WrapResponse(visitor, aimeter.KindOpenAI)
	if _, err := s.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	got := s.Tokens()
	// bytes_in = len(body); Out = bytes_in/4. No prior usage observed → In=0.
	wantOut := int(int64(len(body)) / 4)
	if got.Out != wantOut {
		t.Fatalf("fallback Out: got %d want %d (bytes_in=%d)", got.Out, wantOut, len(body))
	}
	if got.In != 0 {
		t.Fatalf("fallback In: got %d want 0", got.In)
	}
}
