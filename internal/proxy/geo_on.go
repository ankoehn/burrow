//go:build geo

package proxy

// geo_on.go — mmdb-backed GeoLookup (build-tag `geo`, Task 17).
//
// Compiled only with `go build -tags geo`. Wraps
// github.com/oschwald/maxminddb-golang to resolve a visitor IP to its
// ISO-3166-1 alpha-2 country code from a MaxMind-format database file
// (GeoLite2-Country or commercial GeoIP2-Country).
//
// The mmdb file is supplied by the operator via BURROW_GEO_DB_PATH
// (Task 24 plumbing). Burrow never downloads it — MaxMind's terms
// require the operator to accept the EULA + run geoipupdate.
//
// Thread-safety: maxminddb.Reader.Lookup is safe for concurrent use
// after Open; we hold a sync.RWMutex only around the Close path so
// future reload-on-SIGHUP work (post-MVP) can swap readers atomically.

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

// MMDBGeoLookup is the geo-tagged GeoLookup implementation backed by
// MaxMind's mmdb format. Satisfies the GeoLookup interface from
// internal/proxy/ipgeo.go (Task 16).
type MMDBGeoLookup struct {
	mu       sync.RWMutex
	db       *maxminddb.Reader
	path     string
	openedAt time.Time
}

// OpenMMDB opens the mmdb file at path and returns an MMDBGeoLookup
// ready for Country() lookups. Returns the underlying maxminddb error
// (wrapped by the library) on failure — cmd/server should log + treat
// as a fatal startup error when BURROW_GEO_DB_PATH is set and Task 24
// chooses fail-closed semantics.
func OpenMMDB(path string) (*MMDBGeoLookup, error) {
	if path == "" {
		return nil, errors.New("mmdb path is empty")
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &MMDBGeoLookup{
		db:       db,
		path:     path,
		openedAt: time.Now(),
	}, nil
}

// OpenGeoDB is the build-tag-aware factory called by cmd/server (Task
// 24). In the `geo` build it delegates to OpenMMDB; in the default
// build (see geo_off.go) it returns a noop.
func OpenGeoDB(path string) (GeoLookup, error) {
	return OpenMMDB(path)
}

// GeoBuildTagEnabled reports whether the binary was built with
// `-tags geo`. Geo build returns true; default build (see geo_off.go)
// returns false.
func GeoBuildTagEnabled() bool { return true }

// Country resolves ip to its ISO-3166-1 alpha-2 country code. Returns
// ("", nil) when the IP is in the database but has no country record
// (e.g. anonymous-proxy entries), and ("", err) when the lookup fails.
//
// A nil receiver or nil db returns ("", nil) so the engine treats it as
// "unknown" — matching the noop fallback.
func (m *MMDBGeoLookup) Country(ip net.IP) (string, error) {
	if m == nil {
		return "", nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return "", nil
	}
	var rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := m.db.Lookup(ip, &rec); err != nil {
		return "", err
	}
	return rec.Country.ISOCode, nil
}

// Enabled reports whether the lookup has a live mmdb reader.
func (m *MMDBGeoLookup) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.db != nil
}

// DBPath returns the on-disk path of the loaded mmdb file. Useful for
// /healthz diagnostics.
func (m *MMDBGeoLookup) DBPath() string {
	if m == nil {
		return ""
	}
	return m.path
}

// DBAgeSeconds returns the integer seconds since the mmdb file was
// opened. The operator-visible field telling them "your geo database
// is N seconds old" (and therefore due for refresh).
func (m *MMDBGeoLookup) DBAgeSeconds() int64 {
	if m == nil || m.openedAt.IsZero() {
		return 0
	}
	return int64(time.Since(m.openedAt).Seconds())
}

// Close releases the underlying mmdb reader. Safe to call multiple
// times — subsequent Country() calls return ("", nil).
func (m *MMDBGeoLookup) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return nil
	}
	err := m.db.Close()
	m.db = nil
	return err
}
