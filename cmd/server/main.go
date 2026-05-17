// Command burrowd is the Burrow relay server.
//
// `serve` runs the control server: it opens/migrates the SQLite database,
// seeds the first admin from config, authenticates clients against
// DB-issued tokens, and persists registered tunnels. The HTTP API and
// dashboard arrive in MVP Phases 4b/4c.
//
// `token` is an operator/dev helper that mints a client token for an
// existing user directly against the database (no HTTP API needed yet).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/logging"
	"github.com/ankoehn/burrow/internal/server"
	"github.com/ankoehn/burrow/internal/store"
	"github.com/ankoehn/burrow/internal/version"
)

func versionLine() string {
	return fmt.Sprintf("burrowd %s (commit %s, built %s, %s/%s)",
		version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
}

// tunnelStoreAdapter converts the server's *Tunnel to the store's persistence
// shape, so internal/store stays decoupled from internal/server (E8).
type tunnelStoreAdapter struct{ s *store.Store }

func (a tunnelStoreAdapter) SaveTunnel(ctx context.Context, userID string, t *server.Tunnel) error {
	return a.s.SaveTunnel(ctx, userID, &store.SaveTunnelArg{
		ID: t.ID, Name: t.Name, Type: t.Type, RemotePort: t.RemotePort, LocalAddr: t.LocalAddr,
	})
}

func (a tunnelStoreAdapter) MarkTunnelSeen(ctx context.Context, tunnelID string) error {
	return a.s.MarkTunnelSeen(ctx, tunnelID)
}

func main() {
	root := &cobra.Command{
		Use:           "burrowd",
		Short:         "Burrow relay server",
		Version:       versionLine(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the relay control server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			overrides := map[string]any{}
			if v, _ := cmd.Flags().GetString("listen"); v != "" {
				overrides["listen"] = v
			}
			if v, _ := cmd.Flags().GetString("tls-cert"); v != "" {
				overrides["tls_cert"] = v
			}
			if v, _ := cmd.Flags().GetString("tls-key"); v != "" {
				overrides["tls_key"] = v
			}
			cfg, err := config.LoadServer(overrides)
			if err != nil {
				return err
			}
			log := logging.New(cfg.LogLevel, cfg.LogFormat)
			if gen, _ := cmd.Flags().GetBool("dev-certs"); gen {
				if err := devcert.Generate("certs", false); err != nil {
					return err
				}
			}
			database, err := db.Open(cfg.DatabasePath)
			if err != nil {
				return err
			}
			defer database.Close()
			if err := db.Migrate(database); err != nil {
				return err
			}
			st := store.New(database)
			if err := st.SeedAdmin(cmd.Context(), cfg.AdminEmail, cfg.AdminPassword); err != nil {
				return err
			}
			srv, err := server.New(server.Options{
				Listen: cfg.Listen, TLSCert: cfg.TLSCert, TLSKey: cfg.TLSKey,
				PublicBind: cfg.PublicBind, PortMin: cfg.PortMin, PortMax: cfg.PortMax,
				Auth: st, Tunnels: tunnelStoreAdapter{st}, Logger: log,
			})
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := srv.Serve(ctx); err != nil {
				return err
			}
			srv.Wait()
			return nil
		},
	}
	serveCmd.Flags().String("listen", "", "listen address (default :7000)")
	serveCmd.Flags().String("tls-cert", "", "TLS certificate PEM")
	serveCmd.Flags().String("tls-key", "", "TLS key PEM")
	serveCmd.Flags().Bool("dev-certs", false, "generate ./certs dev certs if missing")
	root.AddCommand(serveCmd)

	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Mint a client token for an existing user (dev/operator helper)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			name, _ := cmd.Flags().GetString("name")
			cfg, err := config.LoadServer(nil)
			if err != nil {
				return err
			}
			database, err := db.Open(cfg.DatabasePath)
			if err != nil {
				return err
			}
			defer database.Close()
			if err := db.Migrate(database); err != nil {
				return err
			}
			st := store.New(database)
			u, err := st.GetUserByEmail(cmd.Context(), email)
			if err != nil {
				if err == db.ErrNotFound {
					return fmt.Errorf("no user with email %q (seed an admin via BURROW_ADMIN_EMAIL/PASSWORD and run `serve` once)", email)
				}
				return err
			}
			tok, err := st.IssueClientToken(cmd.Context(), u.ID, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "issued client token %q for %s:\n", name, email)
			fmt.Println(tok)
			return nil
		},
	}
	tokenCmd.Flags().String("email", "", "user email to mint a token for (required)")
	tokenCmd.Flags().String("name", "cli", "token name/label")
	_ = tokenCmd.MarkFlagRequired("email")
	root.AddCommand(tokenCmd)

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionLine())
		},
	})

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
