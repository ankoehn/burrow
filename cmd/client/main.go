// Command burrow is the Burrow local client CLI.
//
// Phase 1: scaffolding stub. It only reports version information; `connect`
// is implemented in MVP Phase 2 (control protocol) and Phase 3 (data plane).
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/version"
)

func versionLine() string {
	return fmt.Sprintf("burrow %s (commit %s, built %s, %s/%s)",
		version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
}

func main() {
	root := &cobra.Command{
		Use:           "burrow",
		Short:         "Burrow local client",
		Version:       versionLine(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "connect",
		Short: "Connect to a Burrow server (not implemented until Phase 2)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("connect: not implemented in Phase 1 scaffolding (see docs/ROADMAP.md)")
		},
	})
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
