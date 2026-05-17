// Command burrowd is the Burrow relay server.
//
// Phase 1: scaffolding stub. It only reports version information; `serve`
// is implemented in MVP Phases 2-5 (control protocol, data plane, API, UI).
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ankoehn/burrow/internal/version"
)

func versionLine() string {
	return fmt.Sprintf("burrowd %s (commit %s, built %s, %s/%s)",
		version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
}

func main() {
	root := &cobra.Command{
		Use:           "burrowd",
		Short:         "Burrow relay server",
		Version:       versionLine(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Run the relay server (not implemented until Phase 2)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("serve: not implemented in Phase 1 scaffolding (see docs/ROADMAP.md)")
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
