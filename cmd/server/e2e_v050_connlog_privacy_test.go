package main

// e2e_v050_connlog_privacy_test.go — Integration Task 9 (v0.5.0):
// Connection-logs Q12 privacy toggle. DEFERRED to v0.5.1.
//
// The v0.5.0 API contract spec (Part E + Q12) documents a
// `connection_logs.rollup_include_top_ips` setting (default true) that
// controls whether the per-day rollup carries a `top_source_ips` field.
// Two things are missing from the v0.5.0 backend:
//
//  1. The setting itself: no row in `settings` for
//     `connection_logs.rollup_include_top_ips`, no PUT path that mutates
//     it, no reader on the rollup compaction side.
//  2. The underlying column / sub-table: `connection_log_rollups` has 8
//     columns (day, service_id, kind, sessions, bytes_in, bytes_out,
//     avg_duration_ms, p95_duration_ms). There is no top_source_ips
//     column and no auxiliary per-day-per-service-per-IP table. The
//     `TopSourceIPs` field only exists today on the webhook payload
//     struct (internal/webhook/dispatcher.go), not on the rollup row.
//
// Shipping the toggle requires (a) a column or aux table on the rollup
// side, (b) a computation step in SQLSink.Rollup that aggregates IPs,
// (c) a `settings` row + handler, and (d) optional exclusion logic on
// the GET /connection-logs/rollups read path. ~150-200 LOC across at
// least four packages — outside the integration plan's surgical budget.
//
// This file pins the executable contract for v0.5.1: when the toggle
// lands, replace the t.Skip with the failing-then-green body suggested
// by docs/superpowers/plans/2026-05-20-v0.5.0-integration.md Task 9.
// Until then the spec amendment for v0.5.0 (release notes) calls Q12
// out as a documented deferral.

import "testing"

// TestConnLogPrivacyTopIPsModes verifies the Q12 mode toggle. Currently
// SKIPPED — see file header for the v0.5.1 deferral rationale.
func TestConnLogPrivacyTopIPsModes(t *testing.T) {
	t.Skip("v0.5.0 P2 deferral: connection_logs.rollup_include_top_ips toggle (Q12) + the underlying top_source_ips rollup column are not yet shipped. Tracked as v0.5.1 P2; see docs/RELEASE_NOTES_v0.5.0.md.")
}
