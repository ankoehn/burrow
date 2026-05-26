# Burrow e2e test coverage matrix

Living doc that maps every Burrow surface / security layer to its tests.
Gitignored under `docs/*`. Updated whenever a new test lands.

Status values:
- ✅ both layers covered (or the layer that applies)
- 🟡 partial (one layer only, or surface-check-only)
- ❌ no test yet
- — not applicable (e.g., HSTS has no UI surface)

See `docs/E2E_SECURITY_COVERAGE_2026-05-26.md` for the design rationale.

| Surface / security layer            | Backend test                                      | UI spec                          | Status |
|-------------------------------------|---------------------------------------------------|----------------------------------|--------|
| Login + session cookie              | TestSec_CookieFlags                               | 01-bootstrap                     | ❌     |
| HTTP tunnel roundtrip               | TestE2EHTTPTunnel_RoundTrip                       | 02-tunnels                       | ✅     |
| HTTP tunnel SSE                     | TestE2EHTTPTunnel_SSEUnbuffered                   | 10-ai-gateway-basic              | ✅     |
| HTTP tunnel WebSocket               | TestE2EHTTPTunnel_WebSocketUpgrade                | —                                | ✅     |
| Unknown subdomain 404               | TestE2EHTTPTunnel_UnknownSubdomain404             | —                                | ✅     |
| Multi-service burrow.yaml           | (implicit)                                        | 03-services-burrow-yaml          | ✅     |
| Tokens mint                         | (implicit)                                        | 04-tokens-mint                   | ✅     |
| Users + roles CRUD                  | (admin handlers cover this)                       | 05-users-roles                   | ✅     |
| Access mode: open                   | (covered by tunnel test)                          | 06-access-mode-open              | ✅     |
| Access mode: api_key                | TestE2EAccessModes_APIKey_DefaultBearer + Custom  | 07-access-mode-api-key           | ✅     |
| Access mode: burrow_login           | TestE2EAccessModes_BurrowLogin_FullFlow           | 08-access-mode-burrow-login      | ✅     |
| Access mode: mTLS                   | TestE2EMTLS_AccessMode                            | 09 (strengthen) + 23             | 🟡     |
| AI gateway basic                    | TestE2EOpenAI_RoundTripAndMetering                | 10-ai-gateway-basic              | ✅     |
| Semantic cache                      | TestV050SemanticCacheE2E                          | 11-ai-gateway-semantic-cache     | ✅     |
| Metering + cost                     | TestE2EOpenAI_RoundTripAndMetering                | 12-ai-gateway-metering-cost      | ✅     |
| Custom domains active               | (partial)                                         | 13 + 31-custom-domains-active    | 🟡     |
| Connection logs                     | TestConnLogPrivacyTopIPsModes                     | 14-connection-logs               | ✅     |
| Audit chain                         | TestE2EAuditChain_FullMutationSequence            | 15-audit-chain                   | ✅     |
| Webhooks delivery                   | TestE2EWebhooks_HMACAndRetry                      | 16 + 30-webhooks-delivery        | 🟡     |
| OpenAPI viewer                      | —                                                 | 17-openapi-viewer                | 🟡     |
| Retention knobs                     | —                                                 | 18-retention                     | 🟡     |
| Postgres swap                       | TestV050PostgresBackendE2E                        | 19-postgres-swap                 | ✅     |
| Relay restart                       | TestE2EFailover_OnConnectionError                 | 20-relay-restart                 | ✅     |
| Dialog a11y                         | —                                                 | 21-dialog-tall                   | 🟡     |
| Clients page + Connect wizard       | —                                                 | 22-clients                       | 🟡     |
| Inspector replay + SSE              | TestE2EInspector_* (4 tests)                      | 24-inspector                     | ❌     |
| Quota rate-limit                    | TestE2EQuota_RateLimit429 + Multi + Day           | 25-quota-rate-limit              | ❌     |
| MCP tools                           | TestE2EMCP_ToolsListAndCall                       | 26-mcp                           | ❌     |
| Failover circuit breaker            | TestE2EFailover_CircuitBreakerTrip                | 27-failover                      | ❌     |
| Backup/restore                      | TestE2EBackup_RestoreRoundtrip                    | 28-backups                       | ❌     |
| IPGeo CIDR/countries                | TestE2EIPGeo_* (2 tests)                          | 29-ipgeo                         | ❌     |
| Metrics endpoint                    | TestE2EMetrics_ClosedSetCoverage                  | —                                | ✅     |
| Cache redaction                     | TestE2ECacheRedact_* (3 tests)                    | (Guardrails page implicit)       | 🟡     |
| HSTS header                         | TestSec_HSTSHeader                                | —                                | ❌     |
| CSRF double-submit                  | TestSec_CSRFRejection                             | —                                | ❌     |
| Cookie flags                        | TestSec_CookieFlags                               | —                                | ❌     |
| Trusted-proxy XFF                   | TestSec_TrustedProxyXFF                           | —                                | ❌     |
| Login rate-limit                    | TestSec_LoginRateLimit                            | 32-login-rate-limit              | ❌     |
| Anthropic adapter                   | TestE2EAnthropic_AdapterRoundTrip                 | —                                | ✅     |
