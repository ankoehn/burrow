//go:build !geo

package proxy

// geo_off.go — default-build GeoLookup factory (Task 17).
//
// The default build (no `-tags geo`) does NOT compile / link the
// github.com/oschwald/maxminddb-golang dependency. OpenGeoDB therefore
// returns the always-noop GeoLookup (already defined in ipgeo.go as
// noopGeoLookup) and signals geo features are disabled at build time
// via an unused mmdb path.
//
// Task 24 wires BURROW_GEO_DB_PATH into cmd/server; this factory lets
// cmd/server call proxy.OpenGeoDB(path) without itself being build-tag
// aware. In the default build the path argument is ignored.

// OpenGeoDB returns a GeoLookup. In the default (no `-tags geo`) build
// the path is ignored and the returned lookup always reports
// Enabled()=false / Country()=("",nil), matching spec Part J's
// "geo build tag off → country filters noop" behaviour.
//
// The `geo` build replaces this with an mmdb-backed implementation
// (see geo_on.go) that opens the file at path and surfaces real
// ISO-3166-1 alpha-2 codes.
func OpenGeoDB(_ string) (GeoLookup, error) {
	return NoopGeoLookup(), nil
}

// GeoBuildTagEnabled reports whether the binary was built with
// `-tags geo`. The default build returns false; geo_on.go overrides
// to true. Useful for /healthz and operator diagnostics in Task 24.
func GeoBuildTagEnabled() bool { return false }
