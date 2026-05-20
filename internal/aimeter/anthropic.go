package aimeter

import (
	"bytes"
	"encoding/json"
)

// anthropicParser handles Anthropic-shaped SSE streams. Frames are pairs of
// the form:
//
//	event: message_delta
//	data:  {"type":"message_delta","usage":{"input_tokens":12,"output_tokens":7}}
//
// followed by a blank separator line. The parser is line-buffered just like
// the OpenAI variant: each completed line is forwarded immediately, then
// inspected. We track the most recent "event:" name so that when a
// subsequent "data:" line arrives we know which envelope shape to expect.
//
// Anthropic streams put usage in TWO places per the SDK:
//   - message_start.message.usage carries an initial input_tokens count.
//   - message_delta.usage carries cumulative output_tokens (and the final
//     input_tokens, which equals the message_start value in most cases).
//
// We record both: input_tokens from whichever event carried a non-zero
// value last, and output_tokens from message_delta.usage.
type anthropicParser struct {
	s         *Stream
	buf       bytes.Buffer
	lastEvent string // most recent "event: NAME" value
}

func newAnthropicParser(s *Stream) *anthropicParser { return &anthropicParser{s: s} }

func (p *anthropicParser) write(b []byte) (forwarded int, err error) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			p.buf.Write(b)
			return forwarded, nil
		}
		var frame []byte
		if p.buf.Len() > 0 {
			p.buf.Write(b[:i+1])
			frame = p.buf.Bytes()
		} else {
			frame = b[:i+1]
		}
		n, werr := p.s.w.Write(frame)
		forwarded += n
		if werr != nil {
			return forwarded, werr
		}
		p.s.flush()
		p.inspect(frame)
		p.buf.Reset()
		b = b[i+1:]
	}
	return forwarded, nil
}

func (p *anthropicParser) close() error {
	if p.buf.Len() == 0 {
		return nil
	}
	n, err := p.s.w.Write(p.buf.Bytes())
	p.s.bytesOut += int64(n)
	p.s.flush()
	p.inspect(p.buf.Bytes())
	p.buf.Reset()
	return err
}

func (p *anthropicParser) inspect(line []byte) {
	trim := bytes.TrimRight(line, "\r\n")
	if len(trim) == 0 {
		return
	}
	switch {
	case bytes.HasPrefix(trim, []byte("event:")):
		p.lastEvent = string(bytes.TrimSpace(trim[len("event:"):]))
	case bytes.HasPrefix(trim, []byte("data:")):
		payload := bytes.TrimSpace(trim[len("data:"):])
		if len(payload) == 0 || payload[0] != '{' {
			return
		}
		switch p.lastEvent {
		case "message_start":
			var env struct {
				Message struct {
					Usage *anthropicUsage `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal(payload, &env); err != nil {
				return
			}
			if env.Message.Usage != nil {
				p.mergeUsage(env.Message.Usage)
			}
		case "message_delta":
			var env struct {
				Usage *anthropicUsage `json:"usage"`
			}
			if err := json.Unmarshal(payload, &env); err != nil {
				return
			}
			if env.Usage != nil {
				p.mergeUsage(env.Usage)
			}
		}
	}
}

// mergeUsage records token counts, preferring non-zero values from the
// most-recent envelope. Anthropic's message_delta.usage is the
// authoritative final count.
func (p *anthropicParser) mergeUsage(u *anthropicUsage) {
	in := u.InputTokens
	out := u.OutputTokens
	// Preserve the previously-observed input_tokens if this envelope doesn't
	// carry one (output-only deltas exist in some streams).
	if in == 0 && p.s.gotUsage {
		in = p.s.tokens.In
	}
	if out == 0 && p.s.gotUsage {
		out = p.s.tokens.Out
	}
	p.s.recordTokens(in, out, 0)
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ParseAnthropicBody parses a fully-buffered non-streaming Anthropic
// /v1/messages response and returns its token counts. Returns the zero
// Tokens if the body is not valid JSON or has no usage object.
func ParseAnthropicBody(body []byte) Tokens {
	var env struct {
		Usage *anthropicUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Usage == nil {
		return Tokens{}
	}
	return Tokens{
		In:    env.Usage.InputTokens,
		Out:   env.Usage.OutputTokens,
		Total: env.Usage.InputTokens + env.Usage.OutputTokens,
	}
}
