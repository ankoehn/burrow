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
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/ankoehn/burrow/internal/proxy"
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

// sessionSnapshotter is the read-only slice of *server.Server the clients
// adapter needs (so the adapter stays unit-testable without a live registry).
type sessionSnapshotter interface {
	SnapshotSessions() []server.SessionSnapshot
}

// tunnelGetter is the read-only slice of *store.Store the clients adapter
// needs for persisted per-tunnel totals + access mode.
type tunnelGetter interface {
	GetTunnel(ctx context.Context, id string) (db.Tunnel, error)
}

// clientsAdapter exposes live sessions + persisted per-service totals to the
// HTTP API (keeps internal/api decoupled from internal/server and internal/db).
type clientsAdapter struct {
	srv sessionSnapshotter
	st  tunnelGetter
}

func (a clientsAdapter) services(ss server.SessionSnapshot) ([]api.ClientServiceView, int64, int64) {
	var svcs []api.ClientServiceView
	var aggIn, aggOut int64
	for _, tn := range ss.Tunnels {
		var totIn, totOut int64
		mode := "open"
		if row, err := a.st.GetTunnel(context.Background(), tn.ID); err == nil {
			totIn, totOut, mode = row.TotalBytesIn, row.TotalBytesOut, row.AccessMode
		}
		aggIn += totIn
		aggOut += totOut
		svcs = append(svcs, api.ClientServiceView{
			ID: tn.ID, Name: tn.Name, Type: tn.Type, RemotePort: tn.RemotePort,
			LocalAddr: tn.LocalAddr, AccessMode: mode,
			BytesIn: tn.BytesIn, BytesOut: tn.BytesOut,
			TotalBytesIn: totIn, TotalBytesOut: totOut,
		})
	}
	return svcs, aggIn, aggOut
}

func (a clientsAdapter) toView(ss server.SessionSnapshot, aggIn, aggOut int64, n int) api.ClientView {
	return api.ClientView{
		SessionID: ss.SessionID, UserID: ss.UserID, TokenName: ss.Token,
		RemoteAddr: ss.RemoteAddr, OS: ss.OS, Arch: ss.Arch,
		ClientVersion: ss.ClientVersion, ServiceCount: n,
		TotalBytesIn: aggIn, TotalBytesOut: aggOut,
	}
}

func (a clientsAdapter) ListClients() []api.ClientView {
	var out []api.ClientView
	for _, ss := range a.srv.SnapshotSessions() {
		_, in, outB := a.services(ss)
		out = append(out, a.toView(ss, in, outB, len(ss.Tunnels)))
	}
	return out
}

func (a clientsAdapter) GetClient(sessionID string) (api.ClientDetail, bool) {
	for _, ss := range a.srv.SnapshotSessions() {
		if ss.SessionID != sessionID {
			continue
		}
		svcs, in, outB := a.services(ss)
		return api.ClientDetail{
			ClientView: a.toView(ss, in, outB, len(ss.Tunnels)),
			Services:   svcs,
		}, true
	}
	return api.ClientDetail{}, false
}

// runTrafficFlusher periodically persists the delta of each live tunnel's
// in-memory byte counters into tunnels.total_bytes_*, so traffic survives
// reconnects. WaitGroup-tracked + ctx-cancelled like runSessionReaper; a final
// flush runs on ctx cancellation before the DB is closed.
func runTrafficFlusher(ctx context.Context, wg *sync.WaitGroup, srv *server.Server, st *store.Store, log *slog.Logger, interval time.Duration) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		last := map[string][2]uint64{} // tunnelID -> {in,out} already persisted
		flush := func() {
			seen := map[string]struct{}{}
			for _, ss := range srv.SnapshotSessions() {
				for _, tn := range ss.Tunnels {
					seen[tn.ID] = struct{}{}
					prev := last[tn.ID]
					dIn := int64(tn.BytesIn) - int64(prev[0])
					dOut := int64(tn.BytesOut) - int64(prev[1])
					if dIn < 0 {
						dIn = int64(tn.BytesIn) // counter reset (reconnect): persist absolute
					}
					if dOut < 0 {
						dOut = int64(tn.BytesOut)
					}
					if dIn == 0 && dOut == 0 {
						continue
					}
					if err := st.FlushTunnelTotals(ctx, tn.ID, dIn, dOut); err != nil {
						log.Warn("traffic flush", "tunnel_id", tn.ID, "err", err)
						continue
					}
					last[tn.ID] = [2]uint64{tn.BytesIn, tn.BytesOut}
				}
			}
			for id := range last { // drop disconnected tunnels
				if _, ok := seen[id]; !ok {
					delete(last, id)
				}
			}
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				flush() // final flush before shutdown / DB close
				return
			case <-t.C:
				flush()
			}
		}
	}()
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
			st.SetSMTPPassword(cfg.SMTPPassword)
			if err := st.SeedAdmin(context.Background(), cfg.AdminEmail, cfg.AdminPassword); err != nil {
				return err
			}
			bus := events.NewBus()
			srv, err := server.New(server.Options{
				Listen: cfg.Listen, TLSCert: cfg.TLSCert, TLSKey: cfg.TLSKey,
				PublicBind: cfg.PublicBind, PortMin: cfg.PortMin, PortMax: cfg.PortMax,
				Auth: st, Tunnels: tunnelStoreAdapter{st}, Events: bus, Logger: log,
				// v0.3.0: HTTP tunnel service identity + subdomain resolver.
				Services:   serviceResolverAdapter{db: db.Wrap(database)},
				AuthDomain: cfg.AuthDomain,
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

			// Traffic flusher: persists live byte counters into
			// tunnels.total_bytes_* every ~30s (and once at shutdown).
			// Tracked on reaperWg so its final flush runs before the
			// deferred database.Close() (LIFO defer ordering).
			runTrafficFlusher(ctx, &reaperWg, srv, st, log, 30*time.Second)

			spaHandler, err := web.Handler()
			if err != nil {
				return err
			}

			// httpsEnabled is true when both HTTP TLS cert+key are configured.
			// effectiveSecureCookies forces Secure on cookies whenever TLS is
			// active (a TLS-served cookie MUST be Secure); the operator-facing
			// http_secure_cookies flag also covers proxy-terminated TLS.
			httpsEnabled := cfg.HTTPTLSCert != "" && cfg.HTTPTLSKey != ""
			effectiveSecureCookies := httpsEnabled || cfg.HTTPSecureCookies

			if !httpsEnabled && !cfg.HTTPSecureCookies {
				log.Warn("dashboard/session cookie is transmitted in plaintext; " +
					"set BURROW_HTTP_TLS_CERT/BURROW_HTTP_TLS_KEY for native HTTPS " +
					"or terminate TLS at a proxy and set BURROW_HTTP_SECURE_COOKIES=true")
			}

			apiSrv := &http.Server{
				Addr: cfg.HTTPListen,
				Handler: api.NewRouter(api.Deps{
					Users: st, Tunnels: tunnelListerAdapter{srv}, Events: bus,
					Log: log, SecureCookies: effectiveSecureCookies, HTTPSEnabled: httpsEnabled,
					SPA: spaHandler, TrustedProxies: cfg.TrustedProxies,
					Roles: st, Sessions: st, Settings: st,
					Clients: clientsAdapter{srv: srv, st: st}, AccessModes: st,
					DB: database,
					// v0.3.0: service API + live tunnel lookup + auth domain.
					Services:    st,
					LiveTunnels: liveTunnelLookupAdapter{srv: srv},
					AuthDomain:  cfg.AuthDomain,
				}),
				ReadHeaderTimeout: 10 * time.Second,
			}
			if httpsEnabled {
				apiSrv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			}

			// senderCount tracks how many goroutines send on errc; capacity must
			// equal senderCount so that no sender ever blocks after shutdown.
			// Baseline: control listener (srv.Serve) + api server = 2.
			// The proxy listener adds 1 when cfg.HTTPProxyListen is non-empty.
			senderCount := 2

			// Build the optional HTTP reverse-proxy listener (v0.3.0).
			// Started only when HTTPProxyListen is non-empty (default ":8443").
			// TLS is used iff both HTTPProxyTLSCert + HTTPProxyTLSKey are set;
			// otherwise the listener runs plain HTTP (operator may terminate TLS
			// upstream — e.g. nginx or a cloud load-balancer). A warning is
			// logged in that case so it is never silently insecure.
			var proxySrv *http.Server
			if cfg.HTTPProxyListen != "" {
				senderCount++

				// Extract the port label for X-Forwarded-Port (e.g. ":8443" → "8443").
				var ingressPort string
				if _, port, err := net.SplitHostPort(cfg.HTTPProxyListen); err == nil && port != "" {
					ingressPort = port
				}

				proxyTLSEnabled := cfg.HTTPProxyTLSCert != "" && cfg.HTTPProxyTLSKey != ""

				accessChecker := proxy.NewAccessCheckerWithLogger(st, cfg.AuthDomain, log)
				gate := proxy.NewGate(st, cfg.AuthDomain, effectiveSecureCookies, log)
				proxyOpts := []proxy.Option{
					proxy.WithGate(gate),
				}
				if ingressPort != "" {
					proxyOpts = append(proxyOpts, proxy.WithIngressPort(ingressPort))
				}
				proxyHandler := proxy.New(
					proxyDialerAdapter{st: st, srv: srv},
					accessChecker,
					cfg.AuthDomain,
					log,
					proxyOpts...,
				)
				proxySrv = &http.Server{
					Addr:              cfg.HTTPProxyListen,
					Handler:           proxyHandler,
					ReadHeaderTimeout: 10 * time.Second,
				}
				if proxyTLSEnabled {
					proxySrv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
				}
			}

			errc := make(chan error, senderCount)
			go func() { errc <- srv.Serve(ctx) }()
			go func() {
				if httpsEnabled {
					log.Info("http api listening (TLS)", "addr", cfg.HTTPListen)
					if err := apiSrv.ListenAndServeTLS(cfg.HTTPTLSCert, cfg.HTTPTLSKey); err != nil && err != http.ErrServerClosed {
						errc <- err
						return
					}
				} else {
					log.Info("http api listening", "addr", cfg.HTTPListen)
					if err := apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						errc <- err
						return
					}
				}
				errc <- nil
			}()
			if proxySrv != nil {
				proxyTLSEnabled := cfg.HTTPProxyTLSCert != "" && cfg.HTTPProxyTLSKey != ""
				go func() {
					if proxyTLSEnabled {
						log.Info("http proxy listening (TLS)", "addr", cfg.HTTPProxyListen)
						if err := proxySrv.ListenAndServeTLS(cfg.HTTPProxyTLSCert, cfg.HTTPProxyTLSKey); err != nil && err != http.ErrServerClosed {
							errc <- err
							return
						}
					} else {
						// Plaintext proxy: operator is expected to terminate TLS upstream
						// (e.g. nginx, cloud load-balancer). Logged as a warning so this
						// configuration is never silently insecure. The listener still
						// starts; use BURROW_HTTP_PROXY_TLS_CERT/KEY for native TLS.
						log.Warn("WARNING: http proxy listener enabled without TLS — http tunnels are unsecured; " +
							"set BURROW_HTTP_PROXY_TLS_CERT/BURROW_HTTP_PROXY_TLS_KEY for native TLS " +
							"or terminate TLS at a proxy")
						if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
							errc <- err
							return
						}
					}
					errc <- nil
				}()
			}

			// Wait for a shutdown signal OR an early server error (e.g. a
			// listener bind failure such as the HTTP port already in use):
			// surface it immediately instead of running half-dead until SIGINT.
			var firstErr error
			select {
			case <-ctx.Done():
			case firstErr = <-errc:
				stop() // cancel ctx so the other (healthy) servers unwind too
			}
			// apiShutdownGrace (35s) > api.JSONHandlerTimeout (30s): every
			// in-flight handler completes (or is chi-cancelled at 30s and
			// returns) before Shutdown returns. The deferred reaperWg.Wait()
			// and database.Close() then run in LIFO order after this point.
			//
			// Shutdown order (reverse of start): proxy → api → control listener (srv).
			// The proxy is shut first so no new tunnel streams are opened while
			// the control listener is still draining.
			shutCtx, cancel := context.WithTimeout(context.Background(), apiShutdownGrace)
			defer cancel()
			if proxySrv != nil {
				_ = proxySrv.Shutdown(shutCtx)
			}
			_ = apiSrv.Shutdown(shutCtx)
			srv.Wait()
			// One value already consumed iff the select took the errc branch;
			// drain the remaining senders so no goroutine leaks.
			remaining := senderCount
			if firstErr != nil {
				remaining = senderCount - 1
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
