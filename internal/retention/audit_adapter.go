package retention

import (
	"context"
	"encoding/json"

	"github.com/ankoehn/burrow/internal/audit"
)

// auditLoggerAdapter bridges *audit.Logger to the AuditLogger interface
// expected by Compactor.  It emits a "retention.compact" event for each
// table the compactor deletes rows from.
type auditLoggerAdapter struct {
	l *audit.Logger
}

// NewAuditLoggerAdapter wraps an *audit.Logger as an AuditLogger suitable
// for passing to New. A nil logger is safe — AppendRetentionCompact becomes
// a no-op.
func NewAuditLoggerAdapter(l *audit.Logger) AuditLogger {
	return &auditLoggerAdapter{l: l}
}

func (a *auditLoggerAdapter) AppendRetentionCompact(ctx context.Context, table string, rowsDeleted int) error {
	if a.l == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"table":        table,
		"rows_deleted": rowsDeleted,
	})
	return a.l.Append(ctx, audit.Event{
		Action:  audit.ActionRetentionCompact,
		Result:  "ok",
		Payload: json.RawMessage(payload),
	})
}
