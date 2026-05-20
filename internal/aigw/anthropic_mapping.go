package aigw

// stopReasonMap translates Anthropic message_delta.delta.stop_reason values
// into OpenAI chat.completion.chunk choices[].finish_reason values per the
// v0.4.0 spec Part Q.1.
//
//	end_turn       → stop
//	max_tokens     → length
//	stop_sequence  → stop
//	tool_use       → tool_calls
//
// Any unrecognised stop_reason is left as the empty string so the emitted
// chunk carries `"finish_reason":""` (callers can decide whether to treat
// that as a hard stop).
var stopReasonMap = map[string]string{
	"end_turn":      "stop",
	"max_tokens":    "length",
	"stop_sequence": "stop",
	"tool_use":      "tool_calls",
}

// Dropped-field name constants. These are surfaced in the `dropped` slice
// returned by RewriteRequest and are referenced in tests, so they're
// centralised here to keep the wire-name discoverable.
const (
	droppedTopK       = "top_k"
	droppedImageInput = "image_input"
)
