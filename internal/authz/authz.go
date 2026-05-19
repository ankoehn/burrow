// Package authz holds Burrow's code-defined role→permission map. In v0.2.0
// only the built-in admin/user roles exist and authorization still keys off
// the role string; this map is the read-only extension seam that custom roles
// (v0.4) will grow from. The :own/:any scope split is committed from day one
// so the permission enum never has to change incompatibly later.
package authz

// Permission is a scoped capability string ("<domain>:<action>[:own|:any]").
type Permission string

// Permission constants. ":own" = act on resources you own; ":any" = act on
// anyone's. Global capabilities (users/roles/settings) have no scope suffix.
const (
	PermTunnelsReadOwn   Permission = "tunnels:read:own"
	PermTunnelsReadAny   Permission = "tunnels:read:any"
	PermTunnelsManageOwn Permission = "tunnels:manage:own"
	PermTunnelsManageAny Permission = "tunnels:manage:any"

	PermTokensManageOwn Permission = "tokens:manage:own"
	PermTokensManageAny Permission = "tokens:manage:any"

	PermServicesConfigureOwn Permission = "services:configure:own"
	PermServicesConfigureAny Permission = "services:configure:any"

	PermSessionsManageOwn Permission = "sessions:manage:own"
	PermSessionsManageAny Permission = "sessions:manage:any"

	PermUsersRead      Permission = "users:read"
	PermUsersManage    Permission = "users:manage"
	PermRolesRead      Permission = "roles:read"
	PermSettingsManage Permission = "settings:manage"
)

// Role is a named permission set (built-in only in v0.2.0).
type Role struct {
	Name        string
	Description string
	Permissions []Permission
}

// adminPerms is every permission (admin = full :any access).
var adminPerms = []Permission{
	PermTunnelsReadAny, PermTunnelsReadOwn,
	PermTunnelsManageAny, PermTunnelsManageOwn,
	PermTokensManageAny, PermTokensManageOwn,
	PermServicesConfigureAny, PermServicesConfigureOwn,
	PermSessionsManageAny, PermSessionsManageOwn,
	PermUsersRead, PermUsersManage,
	PermRolesRead, PermSettingsManage,
}

// userPerms is the standard-user :own subset.
var userPerms = []Permission{
	PermTunnelsReadOwn,
	PermTunnelsManageOwn,
	PermTokensManageOwn,
	PermServicesConfigureOwn,
	PermSessionsManageOwn,
}

var builtin = map[string]Role{
	"admin": {
		Name:        "admin",
		Description: "Full administrative access to all tunnels, client tokens, users, roles, and settings.",
		Permissions: adminPerms,
	},
	"user": {
		Name:        "user",
		Description: "Standard user: manage own tunnels and own client tokens.",
		Permissions: userPerms,
	},
}

// order is the stable presentation order for Roles().
var order = []string{"admin", "user"}

// Roles returns the built-in roles in stable order.
func Roles() []Role {
	out := make([]Role, 0, len(order))
	for _, n := range order {
		out = append(out, builtin[n])
	}
	return out
}

// Get returns the named built-in role and whether it exists.
func Get(name string) (Role, bool) {
	r, ok := builtin[name]
	return r, ok
}

// Can reports whether the named role holds permission p. Unknown roles get
// nothing (safe default).
func Can(role string, p Permission) bool {
	r, ok := builtin[role]
	if !ok {
		return false
	}
	for _, have := range r.Permissions {
		if have == p {
			return true
		}
	}
	return false
}
