package proto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// maxFrameSize caps a single frame payload at 1 MiB to bound memory / DoS.
const maxFrameSize uint32 = 1 << 20

// WriteFrame writes one length-prefixed JSON frame: [4-byte BE len][json].
func WriteFrame(w io.Writer, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if uint32(len(b)) > maxFrameSize {
		return fmt.Errorf("frame too large: %d > %d", len(b), maxFrameSize)
	}
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(b)))
	if _, err := w.Write(hdr); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// ReadFrame reads exactly one frame into env. Rejects len > maxFrameSize.
func ReadFrame(r io.Reader, env *Envelope) error {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr)
	if n > maxFrameSize {
		return fmt.Errorf("frame too large: %d > %d", n, maxFrameSize)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	return json.Unmarshal(body, env)
}

// WriteMessage marshals payload, wraps it in an Envelope, and writes the frame.
func WriteMessage(w io.Writer, typ MessageType, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return WriteFrame(w, Envelope{Type: typ, Payload: raw})
}

// DecodePayload unmarshals an Envelope's payload into v.
func DecodePayload(env Envelope, v any) error {
	return json.Unmarshal(env.Payload, v)
}
