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
