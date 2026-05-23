// retention_handlers.go — GET/PUT /api/v1/settings/retention
//
// GET returns all seven retention settings keys with their current values plus
// an advisory note explaining the audit-chain-safe deletion behavior.
//
// PUT validates each field's range per spec F.1 and persists the updated
// keys via the existing SettingsStore.SaveSettings call.
//
// Permission: admin only (same gate as GET/PUT /api/v1/settings).
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// retentionSettingsResp is the GET /api/v1/settings/retention response shape.
type retentionSettingsResp struct {
	AuditRetentionDays                 int `json:"audit_retention_days"`
	InspectorRetentionCount            int `json:"inspector_retention_count"`
	UsageRetentionDays                 int `json:"usage_retention_days"`
	RedactionRetentionDays             int `json:"redaction_retention_days"`
	ConnectionLogsRetentionDays        int `json:"connection_logs_retention_days"`
	ConnectionLogsRollupsRetentionDays int `json:"connection_logs_rollups_retention_days"`
	WebhookDeliveriesRetentionDays     int `json:"webhook_deliveries_retention_days"`
	// AuditRetentionNote is an advisory string that explains the
	// audit-chain-safe behavior of the compaction job (spec F.2).
	AuditRetentionNote string `json:"audit_retention_note"`
}

const auditRetentionNote = "audit_retention_days controls deletion of the six high-frequency " +
	"leaf action types only (redaction.applied, guardrail.refused, connection.session_summary, " +
	"retention.compact, ai.cache.promoted, ai.upstream_error). Structural audit rows " +
	"(user.create, session.create, etc.) are never deleted to preserve chain integrity. " +
	"Set audit_retention_days to 0 to keep all eligible rows forever."

// retentionSettingsPutReq is the PUT /api/v1/settings/retention request shape.
// All fields are pointers so callers can omit a field to leave it unchanged.
type retentionSettingsPutReq struct {
	AuditRetentionDays                 *int `json:"audit_retention_days"`
	InspectorRetentionCount            *int `json:"inspector_retention_count"`
	UsageRetentionDays                 *int `json:"usage_retention_days"`
	RedactionRetentionDays             *int `json:"redaction_retention_days"`
	ConnectionLogsRetentionDays        *int `json:"connection_logs_retention_days"`
	ConnectionLogsRollupsRetentionDays *int `json:"connection_logs_rollups_retention_days"`
	WebhookDeliveriesRetentionDays     *int `json:"webhook_deliveries_retention_days"`
}

// GetRetentionSettings returns all seven retention settings with their
// current values.  GET /api/v1/settings/retention (admin).
func (d Deps) GetRetentionSettings(w http.ResponseWriter, r *http.Request) {
	m, err := d.Settings.GetSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "get settings failed")
		return
	}
	resp := retentionSettingsResp{
		AuditRetentionDays:                 parseSettingInt(m, "audit.retention_days"),
		InspectorRetentionCount:            parseSettingInt(m, "inspector.retention_count"),
		UsageRetentionDays:                 parseSettingInt(m, "usage.retention_days"),
		RedactionRetentionDays:             parseSettingInt(m, "redaction.retention_days"),
		ConnectionLogsRetentionDays:        parseSettingInt(m, "connection_logs.retention_days"),
		ConnectionLogsRollupsRetentionDays: parseSettingInt(m, "connection_logs.rollups_retention_days"),
		WebhookDeliveriesRetentionDays:     parseSettingInt(m, "webhook_deliveries.retention_days"),
		AuditRetentionNote:                 auditRetentionNote,
	}
	writeJSON(w, http.StatusOK, resp)
}

// PutRetentionSettings validates and persists retention settings.
// PUT /api/v1/settings/retention (admin).
func (d Deps) PutRetentionSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in retentionSettingsPutReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate per spec F.1 ranges.
	if err := validateRetentionReq(in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build the kv map of only the fields the caller provided.
	kv := map[string]string{}
	if in.AuditRetentionDays != nil {
		kv["audit.retention_days"] = strconv.Itoa(*in.AuditRetentionDays)
	}
	if in.InspectorRetentionCount != nil {
		kv["inspector.retention_count"] = strconv.Itoa(*in.InspectorRetentionCount)
	}
	if in.UsageRetentionDays != nil {
		kv["usage.retention_days"] = strconv.Itoa(*in.UsageRetentionDays)
	}
	if in.RedactionRetentionDays != nil {
		kv["redaction.retention_days"] = strconv.Itoa(*in.RedactionRetentionDays)
	}
	if in.ConnectionLogsRetentionDays != nil {
		kv["connection_logs.retention_days"] = strconv.Itoa(*in.ConnectionLogsRetentionDays)
	}
	if in.ConnectionLogsRollupsRetentionDays != nil {
		kv["connection_logs.rollups_retention_days"] = strconv.Itoa(*in.ConnectionLogsRollupsRetentionDays)
	}
	if in.WebhookDeliveriesRetentionDays != nil {
		kv["webhook_deliveries.retention_days"] = strconv.Itoa(*in.WebhookDeliveriesRetentionDays)
	}

	if len(kv) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := d.Settings.SaveSettings(r.Context(), kv); err != nil {
		writeErr(w, http.StatusInternalServerError, "save settings failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateRetentionReq checks every non-nil field in the request against the
// spec F.1 ranges, returning the first validation error or nil.
func validateRetentionReq(in retentionSettingsPutReq) error {
	if in.AuditRetentionDays != nil {
		if v := *in.AuditRetentionDays; v < 0 || v > 3650 {
			return fmt.Errorf("audit_retention_days must be 0..3650 (0 = keep forever), got %d", v)
		}
	}
	if in.InspectorRetentionCount != nil {
		if v := *in.InspectorRetentionCount; v < 1 || v > 1000 {
			return fmt.Errorf("inspector_retention_count must be 1..1000, got %d", v)
		}
	}
	if in.UsageRetentionDays != nil {
		if v := *in.UsageRetentionDays; v < 1 || v > 3650 {
			return fmt.Errorf("usage_retention_days must be 1..3650, got %d", v)
		}
	}
	if in.RedactionRetentionDays != nil {
		if v := *in.RedactionRetentionDays; v < 1 || v > 3650 {
			return fmt.Errorf("redaction_retention_days must be 1..3650, got %d", v)
		}
	}
	if in.ConnectionLogsRetentionDays != nil {
		if v := *in.ConnectionLogsRetentionDays; v < 1 || v > 3650 {
			return fmt.Errorf("connection_logs_retention_days must be 1..3650, got %d", v)
		}
	}
	if in.ConnectionLogsRollupsRetentionDays != nil {
		if v := *in.ConnectionLogsRollupsRetentionDays; v < 0 || v > 3650 {
			return fmt.Errorf("connection_logs_rollups_retention_days must be 0..3650 (0 = keep forever), got %d", v)
		}
	}
	if in.WebhookDeliveriesRetentionDays != nil {
		if v := *in.WebhookDeliveriesRetentionDays; v < 1 || v > 365 {
			return fmt.Errorf("webhook_deliveries_retention_days must be 1..365, got %d", v)
		}
	}
	return nil
}

// parseSettingInt reads an integer setting from a map of strings, returning 0
// on missing or unparseable values.
func parseSettingInt(m map[string]string, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}
