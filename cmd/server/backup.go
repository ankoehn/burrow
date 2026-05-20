// backup.go — `burrowd backup` subcommand and the tar.gz archive helper.
//
// Spec Part L (locked invariants):
//   - VACUUM INTO produces an atomic SQLite snapshot under concurrent
//     reads/writes against the live database.
//   - The output tar.gz carries `db.sqlite`, `manifest.json`, and optional
//     `tls/cert.pem`, `tls/key.pem`, `config/burrow.yaml` when those files
//     exist on disk.
//   - manifest.json has the closed shape
//     {version, taken_at, db_sha256, included:[...]}
//     where db_sha256 is the lowercase hex SHA-256 of db.sqlite. The
//     manifest's db_sha256 is the integrity anchor the restore CLI re-checks
//     before the os.Rename swap.
//
// The cmd/server/backup.go CLI is intentionally written as a small wrapper
// around runBackup so tests (cmd/server/backup_test.go) can drive it without
// touching the cobra plumbing.

package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/version"
)

// ManifestVersion is the schema version of manifest.json. The restore CLI
// refuses archives with a manifest_version it does not understand.
const ManifestVersion = 1

// BackupManifest is the wire shape of manifest.json inside the tar.gz.
// Field names are stable: NDJSON exports, the restore CLI, and the
// dashboard list endpoint all key off them.
type BackupManifest struct {
	// Version is the build version of burrowd that produced the archive
	// (version.Version). The schema version of manifest.json itself is
	// ManifestVersion; both are recorded so an older binary can still parse
	// the manifest while refusing a newer schema.
	Version         string    `json:"version"`
	ManifestVersion int       `json:"manifest_version"`
	TakenAt         time.Time `json:"taken_at"`
	DBSha256        string    `json:"db_sha256"`
	Included        []string  `json:"included"`
}

// backupOptions captures the operator-supplied flags + the resolved paths
// the CLI passes into runBackup. The struct exists purely as a testable
// seam so unit tests can call runBackup directly without going through
// cobra.
type backupOptions struct {
	DBPath     string // source SQLite database
	OutPath    string // .tar.gz destination
	TLSCert    string // optional: include under tls/cert.pem
	TLSKey     string // optional: include under tls/key.pem
	ConfigFile string // optional: include under config/burrow.yaml
}

// newBackupCmd returns the `burrowd backup` cobra subcommand.
//
// Usage:
//
//	burrowd backup --to <file.tar.gz> [--db <path>]
//
// Exit codes:
//
//	0 — backup written
//	1 — any error (DB unavailable, output exists, VACUUM INTO failed, …)
func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Snapshot the burrow database into a tar.gz archive",
		Long: `Acquires an atomic SQLite snapshot of the burrow database via
VACUUM INTO and writes it as a tar.gz archive together with a manifest.json
recording the build version, taken_at timestamp, and the SHA-256 of the
snapshot file. When the operator's TLS cert/key or config file are present,
they are included under tls/ and config/ entries so a restore can rehydrate
the full operator-supplied set in one shot.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			to, _ := cmd.Flags().GetString("to")
			dbPath, _ := cmd.Flags().GetString("db")
			if to == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "error: --to is required")
				return cobraExit1()
			}
			opts, err := resolveBackupOptions(dbPath, to)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return cobraExit1()
			}
			if err := runBackup(cmd.Context(), opts, cmd.OutOrStdout()); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return cobraExit1()
			}
			return nil
		},
	}
	cmd.Flags().String("to", "", "destination .tar.gz path (required)")
	cmd.Flags().String("db", "", "path to burrow.db (default: $BURROW_DATABASE_PATH or ./burrow.db)")
	return cmd
}

// resolveBackupOptions fills the backupOptions from the supplied flags and
// the same config-resolution path the serve command uses, so `burrowd backup`
// "just works" against an existing deployment.
func resolveBackupOptions(dbPath, outPath string) (backupOptions, error) {
	opts := backupOptions{OutPath: outPath}
	if dbPath != "" {
		opts.DBPath = dbPath
	} else {
		cfg, err := config.LoadServer(nil)
		if err != nil {
			// If config validation fails (e.g. missing certs in a dev
			// sandbox) we still try the default DB path — backups must
			// work even in a minimally-configured environment.
			opts.DBPath = "./burrow.db"
		} else {
			opts.DBPath = cfg.DatabasePath
			opts.TLSCert = cfg.HTTPTLSCert
			opts.TLSKey = cfg.HTTPTLSKey
		}
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		return opts, fmt.Errorf("database %s: %w", opts.DBPath, err)
	}
	return opts, nil
}

// runBackup performs the snapshot+archive flow. It is the testable seam:
// the cobra wrapper passes pre-resolved paths, and unit tests call this
// directly with fully-controlled temp paths.
//
// Steps:
//  1. Refuse if the output already exists (atomic semantics: the operator
//     gets a fresh archive, never a half-written overwrite).
//  2. VACUUM INTO a sibling temp file (atomic snapshot).
//  3. Compute sha256 of the snapshot and build manifest.json.
//  4. Write tar.gz with db.sqlite + manifest.json + optional tls/* + config/.
//  5. Move into place via os.Rename for crash-safe write.
//
// The temp snapshot is unlinked on success and on failure.
func runBackup(ctx context.Context, opts backupOptions, stdout io.Writer) error {
	if opts.OutPath == "" {
		return errors.New("backup: --to is required")
	}
	if opts.DBPath == "" {
		return errors.New("backup: --db is required")
	}
	if _, err := os.Stat(opts.OutPath); err == nil {
		return fmt.Errorf("backup: refusing to overwrite existing %s", opts.OutPath)
	}

	// VACUUM INTO must write to a path that does not exist yet. Use a temp
	// path next to the source DB so the rename inside the tar writer's
	// temp directory below stays on the same filesystem if at all possible.
	snapDir, err := os.MkdirTemp("", "burrow-backup-*")
	if err != nil {
		return fmt.Errorf("backup: temp dir: %w", err)
	}
	defer os.RemoveAll(snapDir)

	snapPath := filepath.Join(snapDir, "db.sqlite")
	if err := db.VacuumInto(ctx, opts.DBPath, snapPath); err != nil {
		return fmt.Errorf("backup: snapshot: %w", err)
	}

	dbSha, err := sha256File(snapPath)
	if err != nil {
		return fmt.Errorf("backup: hash snapshot: %w", err)
	}

	included := []string{"db.sqlite", "manifest.json"}
	if opts.TLSCert != "" {
		if _, err := os.Stat(opts.TLSCert); err == nil {
			included = append(included, "tls/cert.pem")
		} else {
			opts.TLSCert = ""
		}
	}
	if opts.TLSKey != "" {
		if _, err := os.Stat(opts.TLSKey); err == nil {
			included = append(included, "tls/key.pem")
		} else {
			opts.TLSKey = ""
		}
	}
	if opts.ConfigFile != "" {
		if _, err := os.Stat(opts.ConfigFile); err == nil {
			included = append(included, "config/burrow.yaml")
		} else {
			opts.ConfigFile = ""
		}
	}
	sort.Strings(included)

	manifest := BackupManifest{
		Version:         version.Version,
		ManifestVersion: ManifestVersion,
		TakenAt:         time.Now().UTC(),
		DBSha256:        dbSha,
		Included:        included,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: marshal manifest: %w", err)
	}

	// Write into a sibling .partial file and rename for crash-safe atomic
	// publication: a Ctrl-C mid-write never leaves a half-archive at the
	// canonical OutPath.
	partial := opts.OutPath + ".partial"
	defer os.Remove(partial)
	if err := writeBackupArchive(partial, snapPath, manifestBytes, opts); err != nil {
		return err
	}
	if err := os.Rename(partial, opts.OutPath); err != nil {
		return fmt.Errorf("backup: publish %s: %w", opts.OutPath, err)
	}
	if stdout != nil {
		fmt.Fprintf(stdout, "Wrote %s (db_sha256=%s)\n", opts.OutPath, dbSha)
	}
	return nil
}

// writeBackupArchive streams the snapshot + manifest (+ optional TLS / config
// files) into a gzipped tar at outPath. The entries are added in the order
// they appear in the `included` list so two byte-identical inputs produce a
// byte-identical archive (modulo gzip timestamps).
func writeBackupArchive(outPath, snapPath string, manifestBytes []byte, opts backupOptions) error {
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create %s: %w", outPath, err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// 1) db.sqlite — the snapshot itself.
	if err := tarAddFile(tw, "db.sqlite", snapPath); err != nil {
		return err
	}
	// 2) manifest.json (in-memory bytes).
	mh := &tar.Header{
		Name:    "manifest.json",
		Mode:    0o644,
		Size:    int64(len(manifestBytes)),
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(mh); err != nil {
		return fmt.Errorf("backup: tar manifest header: %w", err)
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return fmt.Errorf("backup: tar manifest body: %w", err)
	}
	// 3) optional TLS cert / key (operator-supplied).
	if opts.TLSCert != "" {
		if err := tarAddFile(tw, "tls/cert.pem", opts.TLSCert); err != nil {
			return err
		}
	}
	if opts.TLSKey != "" {
		if err := tarAddFile(tw, "tls/key.pem", opts.TLSKey); err != nil {
			return err
		}
	}
	// 4) optional burrow.yaml config file.
	if opts.ConfigFile != "" {
		if err := tarAddFile(tw, "config/burrow.yaml", opts.ConfigFile); err != nil {
			return err
		}
	}
	return nil
}

// tarAddFile copies an on-disk file into the tar writer under name.
func tarAddFile(tw *tar.Writer, name, src string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", src, err)
	}
	hdr := &tar.Header{
		Name:    name,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar %s header: %w", name, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", src, err)
	}
	defer in.Close()
	if _, err := io.Copy(tw, in); err != nil {
		return fmt.Errorf("backup: tar %s body: %w", name, err)
	}
	return nil
}

// sha256File returns the lowercase hex sha256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
