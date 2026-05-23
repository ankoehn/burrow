package retention

import (
	"context"
	"strconv"

	"github.com/ankoehn/burrow/internal/db"
)

// DBLoader implements Loader by reading the retention keys from the settings
// table via *db.DB.
type DBLoader struct {
	b *db.DB
}

// NewDBLoader returns a Loader backed by the given database.
func NewDBLoader(b *db.DB) *DBLoader { return &DBLoader{b: b} }

// Load reads all settings rows and maps the retention keys to a Settings struct.
// Missing keys fall back to zero (i.e. "keep forever" / disabled).
func (l *DBLoader) Load(ctx context.Context) (Settings, error) {
	rows, err := l.b.GetAllSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return Settings{
		AuditDays:                parseInt(m["audit.retention_days"]),
		UsageDays:                parseInt(m["usage.retention_days"]),
		RedactionDays:            parseInt(m["redaction.retention_days"]),
		ConnectionLogsDays:       parseInt(m["connection_logs.retention_days"]),
		ConnectionLogRollupsDays: parseInt(m["connection_logs.rollups_retention_days"]),
		WebhookDeliveriesDays:    parseInt(m["webhook_deliveries.retention_days"]),
		InspectorCount:           parseInt(m["inspector.retention_count"]),
	}, nil
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
}
