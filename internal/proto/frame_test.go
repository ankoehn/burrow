package proto

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Envelope{Type: MsgPing, ID: "abc", Payload: []byte(`{"x":1}`)}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	var out Envelope
	if err := ReadFrame(&buf, &out); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if out.Type != in.Type || out.ID != in.ID || string(out.Payload) != string(in.Payload) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", out, in)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	var buf bytes.Buffer
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, maxFrameSize+1)
	buf.Write(hdr)
	var out Envelope
	if err := ReadFrame(&buf, &out); err == nil || !strings.Contains(err.Error(), "frame too large") {
		t.Fatalf("expected frame-too-large error, got %v", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	var buf bytes.Buffer
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, 10)
	buf.Write(hdr)
	buf.Write([]byte("short"))
	var out Envelope
	if err := ReadFrame(&buf, &out); err == nil || err == io.EOF {
		t.Fatalf("expected unexpected-EOF error, got %v", err)
	}
}

func TestWriteMessageReadMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, MsgAuthRequest, AuthRequest{ProtocolVersion: 1, Token: "t"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	var env Envelope
	if err := ReadFrame(&buf, &env); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if env.Type != MsgAuthRequest {
		t.Fatalf("type=%s", env.Type)
	}
	var ar AuthRequest
	if err := DecodePayload(env, &ar); err != nil || ar.Token != "t" || ar.ProtocolVersion != 1 {
		t.Fatalf("decode: %+v err=%v", ar, err)
	}
}
