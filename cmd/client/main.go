// Command burrow is the Burrow local client CLI.
//
// `connect` runs the Phase 2 control client (TLS auth, tunnel registration,
// heartbeat, auto-reconnect); the TCP data plane arrives in MVP Phase 3.
package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/client"
	"github.com/ankoehn/burrow/internal/config"
	"github.com/ankoehn/burrow/internal/logging"
	"github.com/ankoehn/burrow/internal/version"
)

func versionLine() string {
	return fmt.Sprintf("burrow %s (commit %s, built %s, %s/%s)",
		version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
}

// buildTunnelSpec constructs a client.TunnelSpec from the provided flags values,
// validating that typ is one of "tcp" or "http".
func buildTunnelSpec(name string, remotePort int, localAddr string, typ string) (client.TunnelSpec, error) {
	if typ != "tcp" && typ != "http" {
		return client.TunnelSpec{}, fmt.Errorf("unknown tunnel type %q: must be tcp or http", typ)
	}
	return client.TunnelSpec{
		Name:       name,
		Type:       typ,
		RemotePort: remotePort,
		LocalAddr:  localAddr,
	}, nil
}

// newConnectCmd constructs the "connect" sub-command.
func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to a Burrow server and register a tunnel",
		RunE: func(cmd *cobra.Command, _ []string) error {
			server, _ := cmd.Flags().GetString("server")
			token, _ := cmd.Flags().GetString("token")
			local, _ := cmd.Flags().GetString("local")
			remote, _ := cmd.Flags().GetInt("remote")
			name, _ := cmd.Flags().GetString("name")
			insecure, _ := cmd.Flags().GetBool("insecure")
			caPath, _ := cmd.Flags().GetString("cacert")
			serverName, _ := cmd.Flags().GetString("server-name")
			typ, _ := cmd.Flags().GetString("type")

			spec, err := buildTunnelSpec(name, remote, local, typ)
			if err != nil {
				return err
			}

			cfg, err := config.LoadClient(map[string]any{
				"server": server, "token": token, "insecure": insecure,
				"cacert": caPath, "server_name": serverName,
			})
			if err != nil {
				return err
			}
			log := logging.New(cfg.LogLevel, cfg.LogFormat)
			var pool *x509.CertPool
			if cfg.CACert != "" {
				pem, err := os.ReadFile(cfg.CACert)
				if err != nil {
					return err
				}
				pool = x509.NewCertPool()
				if !pool.AppendCertsFromPEM(pem) {
					return fmt.Errorf("cacert %s: no certificates", cfg.CACert)
				}
			}
			sn := cfg.ServerName
			if sn == "" {
				if h, _, e := net.SplitHostPort(cfg.Server); e == nil {
					sn = h
				}
			}
			cl := client.New(client.Options{
				Server: cfg.Server, Token: cfg.Token, Insecure: cfg.Insecure,
				RootCAs: pool, ServerName: sn, Logger: log,
				Tunnels: []client.TunnelSpec{spec},
			})
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return cl.Run(ctx)
		},
	}
	cmd.Flags().String("server", "", "server host:port (required)")
	cmd.Flags().String("token", "", "auth token (required)")
	cmd.Flags().String("local", "127.0.0.1:3000", "local address to expose")
	cmd.Flags().Int("remote", 0, "requested remote port (0 = auto)")
	cmd.Flags().String("name", "", "tunnel name")
	cmd.Flags().Bool("insecure", false, "skip TLS verification (DEV ONLY)")
	cmd.Flags().String("cacert", "", "PEM CA to trust (e.g. certs/dev-ca.pem)")
	cmd.Flags().String("server-name", "", "TLS SNI/verify name (default: host of --server)")
	cmd.Flags().String("type", "tcp", "tunnel type: tcp|http (--remote is ignored for http)")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("token")
	return cmd
}

func main() {
	root := &cobra.Command{
		Use:           "burrow",
		Short:         "Burrow local client",
		Version:       versionLine(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newConnectCmd())

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
