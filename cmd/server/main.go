// Command burrowd is the Burrow relay server.
//
// `serve` runs the control server: it opens/migrates the SQLite database,
// seeds the first admin from config, authenticates clients against
// DB-issued tokens, and persists registered tunnels. It ALSO serves the
// embedded dashboard SPA at / (the web UI) alongside the HTTP JSON API +
// SSE on BURROW_HTTP_LISTEN (default :8080), beside the control listener.
//
// `token` is an operator/dev helper that mints a client token for an
// existing user directly against the database (no HTTP API needed yet).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
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
	"github.com/ankoehn/burrow/web"
)

// apiShutdownGrace is the timeout given to (*http.Server).Shutdown when the
// serve command receives a stop signal. It must be strictly greater than
// api.JSONHandlerTimeout (the chi middleware.Timeout applied to JSON routes)
// so that every in-flight handler has time to complete (or be chi-cancelled)
// before Shutdown returns and the deferred database.Close() runs.
const apiShutdownGrace = api.JSONHandlerTimeout + 5*time.Second

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

// sessionReaper is the type used to purge expired sessions.
// Using an interface makes the reaper testable without a real DB.
type sessionReaper interface {
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// runSessionReaper starts a goroutine that calls reaper.DeleteExpiredSessions
// immediately and then every interval. It mirrors the byteTicker pattern in
// internal/server: the goroutine is tracked on wg and exits when ctx is done.
// Callers must wg.Wait() before closing the database.
func runSessionReaper(ctx context.Context, wg *sync.WaitGroup, reaper sessionReaper, log *slog.Logger, interval time.Duration) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		purge := func() {
			n, err := reaper.DeleteExpiredSessions(ctx)
			if err != nil {
				log.Warn("session reaper", "err", err)
				return
			}
			if n > 0 {
				log.Info("session reaper: purged expired sessions", "count", n)
			}
		}
		purge() // run once at startup
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				purge()
			}
		}
	}()
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
			if isDev, reason := server.DevCertWarning(cfg.TLSCert); isDev {
				log.Warn("serving with a DEVELOPMENT self-signed TLS certificate — NOT for production; set BURROW_TLS_CERT/BURROW_TLS_KEY (or --tls-cert/--tls-key) to real certificates",
					"reason", reason, "cert", cfg.TLSCert)
			}
			database, err := db.Open(cfg.DatabasePath)
			if err != nil {
				return err
			}
			defer database.Close()
			// reaperWg tracks the session-reaper goroutine; it is waited (via
			// defer below) before database.Close() runs (LIFO defer ordering).
			var reaperWg sync.WaitGroup
			defer reaperWg.Wait()
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

			// Start the session reaper: purges expired sessions once at startup
			// and then every hour. Mirrors the byteTicker goroutine pattern in
			// internal/server: WaitGroup-tracked, ctx-cancelled, stops before
			// database.Close() (enforced by LIFO defers above).
			runSessionReaper(ctx, &reaperWg, db.Wrap(database), log, time.Hour)

			spaHandler, err := web.Handler()
			if err != nil {
				return err
			}

			apiSrv := &http.Server{
				Addr: cfg.HTTPListen,
				Handler: api.NewRouter(api.Deps{
					Users: st, Tunnels: tunnelListerAdapter{srv}, Events: bus,
					Log: log, SecureCookies: cfg.HTTPSecureCookies, SPA: spaHandler,
					TrustedProxies: cfg.TrustedProxies,
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
			// apiShutdownGrace (35s) > api.JSONHandlerTimeout (30s): every
			// in-flight handler completes (or is chi-cancelled at 30s and
			// returns) before Shutdown returns. The deferred reaperWg.Wait()
			// and database.Close() then run in LIFO order after this point.
			shutCtx, cancel := context.WithTimeout(context.Background(), apiShutdownGrace)
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
