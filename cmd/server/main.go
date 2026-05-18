// Command burrowd is the Burrow relay server.
//
// `serve` runs the control server: it opens/migrates the SQLite database,
// seeds the first admin from config, authenticates clients against
// DB-issued tokens, and persists registered tunnels. It ALSO serves the
// HTTP JSON API + SSE on BURROW_HTTP_LISTEN (default :8080) alongside the
// control listener; the dashboard (web UI) arrives in MVP Phase 4c.
//
// `token` is an operator/dev helper that mints a client token for an
// existing user directly against the database (no HTTP API needed yet).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/devcert"
	"github.com/ankoehn/burrow/internal/events"
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

// userTunnelLister is the minimal slice of *server.Server that
// tunnelListerAdapter needs. Depending on this interface (rather than the
// concrete *server.Server, which it still satisfies) keeps the adapter's
// server.TunnelView -> api.TunnelView field mapping unit-testable in
// package main without driving a full TLS+yamux handshake to populate the
// server's unexported registry.
type userTunnelLister interface {
	ListUserTunnels(userID string) []server.TunnelView
}

// tunnelListerAdapter exposes the live server registry to the HTTP API,
// converting server.TunnelView to api.TunnelView (keeps internal/api
// decoupled from internal/server, same pattern as tunnelStoreAdapter).
type tunnelListerAdapter struct{ s userTunnelLister }

func (a tunnelListerAdapter) ListUserTunnels(userID string) []api.TunnelView {
	var out []api.TunnelView
	for _, t := range a.s.ListUserTunnels(userID) {
		out = append(out, api.TunnelView{
			ID: t.ID, Name: t.Name, Type: t.Type, RemotePort: t.RemotePort,
			LocalAddr: t.LocalAddr, BytesIn: t.BytesIn, BytesOut: t.BytesOut, Connected: t.Connected,
		})
	}
	return out
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
			if err := st.SeedAdmin(context.Background(), cfg.AdminEmail, cfg.AdminPassword); err != nil {
				return err
			}
			bus := events.NewBus()
			srv, err := server.New(server.Options{
				Listen: cfg.Listen, TLSCert: cfg.TLSCert, TLSKey: cfg.TLSKey,
				PublicBind: cfg.PublicBind, PortMin: cfg.PortMin, PortMax: cfg.PortMax,
				Auth: st, Tunnels: tunnelStoreAdapter{st}, Events: bus, Logger: log,
			})
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			apiSrv := &http.Server{
				Addr: cfg.HTTPListen,
				Handler: api.NewRouter(api.Deps{
					Users: st, Tunnels: tunnelListerAdapter{srv}, Events: bus,
					Log: log, SecureCookies: cfg.HTTPSecureCookies,
				}),
				ReadHeaderTimeout: 10 * time.Second,
			}

			errc := make(chan error, 2)
			go func() { errc <- srv.Serve(ctx) }()
			go func() {
				log.Info("http api listening", "addr", cfg.HTTPListen)
				if err := apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					errc <- err
					return
				}
				errc <- nil
			}()

			// Wait for a shutdown signal OR an early server error (e.g. a
			// listener bind failure such as the HTTP port already in use):
			// surface it immediately instead of running half-dead until SIGINT.
			var firstErr error
			select {
			case <-ctx.Done():
			case firstErr = <-errc:
				stop() // cancel ctx so the other (healthy) server unwinds too
			}
			// 5s is intentionally shorter than the JSON group's 30s chi
			// middleware.Timeout; database.Close() (deferred earliest) runs
			// only AFTER srv.Wait() below, so do not widen this asymmetry
			// without revisiting the BACKLOG "API drain before DB close" item.
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = apiSrv.Shutdown(shutCtx)
			srv.Wait()
			// One value already consumed iff the select took the errc branch;
			// drain the remaining sender so neither goroutine leaks.
			remaining := 2
			if firstErr != nil {
				remaining = 1
			}
			for i := 0; i < remaining; i++ {
				if e := <-errc; e != nil && e != http.ErrServerClosed && firstErr == nil {
					firstErr = e
				}
			}
			if firstErr != nil && firstErr != http.ErrServerClosed {
				return firstErr
			}
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
			u, err := st.GetUserByEmail(context.Background(), email)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					return fmt.Errorf("no user with email %q (seed an admin via BURROW_ADMIN_EMAIL/PASSWORD and run `serve` once)", email)
				}
				return err
			}
			tok, err := st.IssueClientToken(context.Background(), u.ID, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "issued token named %s for %s:\n", name, email)
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
