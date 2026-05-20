package authz

import "testing"

func TestPermsV040(t *testing.T) {
	for _, p := range []Permission{
		PermAIReadAny, PermAIConfigureAny, PermAuditRead,
		PermWebhooksManage, PermRolesManage, PermBackupRun,
		PermMetricsRead, PermMtlsManageAny, PermIPGeoManageAny,
		PermInspectorReadAny, PermInspectorReplayAny,
		PermAutomationTokensManageAny, PermQuotasManageAny,
		PermMcpToolsRead,
	} {
		if !Can("admin", p) {
			t.Fatalf("admin should have %s", p)
		}
	}
	for _, p := range []Permission{
		PermAIReadOwn, PermAIConfigureOwn, PermInspectorReadOwn,
		PermInspectorReplayOwn, PermAutomationTokensManageOwn,
		PermQuotasReadOwn, PermMtlsManageOwn, PermIPGeoManageOwn,
	} {
		if !Can("user", p) {
			t.Fatalf("user should have %s", p)
		}
	}
}

func TestBuiltinRoles(t *testing.T) {
	rs := Roles()
	if len(rs) != 2 {
		t.Fatalf("want 2 builtin roles, got %d", len(rs))
	}
	if _, ok := Get("admin"); !ok {
		t.Fatal("admin role missing")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown role must not resolve")
	}
}

// TestCustomRoleCache covers SetRoles + Can() for non-builtin roles. After
// SetRoles publishes {"analyst": [ai:read:any]}, Can("analyst", PermAIReadAny)
// must return true; passing the empty map restores the deny-default. The
// builtin admin/user paths must remain unaffected throughout.
func TestCustomRoleCache(t *testing.T) {
	// Reset at end so other tests see a clean cache.
	defer SetRoles(nil)

	// Empty state: any custom role is unknown.
	if Can("analyst", PermAIReadAny) {
		t.Fatal("analyst must deny before SetRoles")
	}

	SetRoles(map[string][]Permission{
		"analyst": {PermAIReadAny, PermAuditRead},
	})
	if !Can("analyst", PermAIReadAny) {
		t.Fatal("analyst should hold ai:read:any after SetRoles")
	}
	if !Can("analyst", PermAuditRead) {
		t.Fatal("analyst should hold audit:read after SetRoles")
	}
	if Can("analyst", PermUsersManage) {
		t.Fatal("analyst must NOT hold users:manage (not in its set)")
	}

	// Builtin admin/user must still resolve from the hard-coded table.
	if !Can("admin", PermUsersManage) {
		t.Fatal("builtin admin path must be unaffected by SetRoles")
	}
	if Can("user", PermUsersManage) {
		t.Fatal("builtin user path must be unaffected by SetRoles")
	}

	// Attempting to overwrite a builtin via SetRoles is a silent no-op.
	SetRoles(map[string][]Permission{"admin": {}, "analyst": {PermAuditRead}})
	if !Can("admin", PermUsersManage) {
		t.Fatal("builtin admin must not be shadowed by SetRoles")
	}
	if Can("analyst", PermAIReadAny) {
		t.Fatal("analyst's perm set should have shrunk to {audit:read} only")
	}
	if !Can("analyst", PermAuditRead) {
		t.Fatal("analyst should still hold audit:read after refresh")
	}
}

func TestCanScopes(t *testing.T) {
	cases := []struct {
		role string
		p    Permission
		want bool
	}{
		{"admin", PermTunnelsManageAny, true},
		{"admin", PermUsersManage, true},
		{"admin", PermSettingsManage, true},
		{"admin", PermServicesConfigureAny, true},
		{"admin", PermTunnelsManageOwn, true},
		{"user", PermTunnelsReadOwn, true},
		{"user", PermTunnelsManageOwn, true},
		{"user", PermTokensManageOwn, true},
		{"user", PermServicesConfigureOwn, true},
		{"user", PermSessionsManageOwn, true},
		{"user", PermTunnelsManageAny, false},
		{"user", PermUsersManage, false},
		{"user", PermUsersRead, false},
		{"user", PermSettingsManage, false},
		{"user", PermServicesConfigureAny, false},
		{"nope", PermTunnelsReadOwn, false},
	}
	for _, c := range cases {
		if got := Can(c.role, c.p); got != c.want {
			t.Errorf("Can(%q,%q)=%v want %v", c.role, c.p, got, c.want)
		}
	}
}
