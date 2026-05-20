// backup.go — SQLite VACUUM INTO helper for the burrowd backup CLI.
//
// VacuumInto is the atomic snapshot primitive used by the backup CLI and the
// dashboard backup endpoints. It opens a fresh sql.DB pointed at srcPath
// (read-only access is sufficient at the OS level — sqlite's VACUUM INTO
// only needs a writable file at dstPath) and executes
//
//	VACUUM INTO 'dstPath';
//
// which produces a fully consistent on-disk copy of the source database.
// The operation is safe under concurrent reads/writes against the original
// database file (spec Part L invariant): SQLite holds a write lock only on
// the destination file, not the source. modernc.org/sqlite (Burrow's pure-Go
// driver) supports VACUUM INTO since the underlying SQLite 3.27+ baseline
// it tracks, so this lands without driver work.

package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// VacuumInto produces an atomic copy of the SQLite database at srcPath into
// dstPath using "VACUUM INTO". dstPath MUST NOT exist (sqlite refuses to
// overwrite); callers are expected to write to a temp path and then rename.
//
// The source database is opened fresh so the helper is safe to call against
// the same on-disk file the live server is using — VACUUM INTO holds only
// a brief read transaction on the source while it writes the target.
func VacuumInto(ctx context.Context, srcPath, dstPath string) error {
	if srcPath == "" {
		return fmt.Errorf("db: VacuumInto: src path is required")
	}
	if dstPath == "" {
		return fmt.Errorf("db: VacuumInto: dst path is required")
	}
	if strings.ContainsAny(dstPath, "'") {
		// VACUUM INTO uses a string literal; refuse a path containing a
		// single quote rather than try to escape it (operator-controlled).
		return fmt.Errorf("db: VacuumInto: dst path %q contains a single quote", dstPath)
	}
	d, err := sql.Open("sqlite", srcPath)
	if err != nil {
		return fmt.Errorf("db: VacuumInto: open %s: %w", srcPath, err)
	}
	defer d.Close()
	// Use a single connection so the VACUUM INTO command runs against the
	// same handle that any subsequent PRAGMAs would, matching db.Open's
	// SetMaxOpenConns(1).
	d.SetMaxOpenConns(1)
	if _, err := d.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", dstPath)); err != nil {
		return fmt.Errorf("db: VACUUM INTO %q: %w", dstPath, err)
	}
	return nil
}
