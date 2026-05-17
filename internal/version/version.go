// Package version exposes build metadata, populated at link time via -ldflags.
package version

// These values are overridden at build time, e.g.:
//
//	go build -ldflags "-X github.com/ankoehn/burrow/internal/version.Version=v0.1.0"
var (
	// Version is the semantic version or `git describe` of the build.
	Version = "dev"
	// Commit is the git commit hash of the build.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"
)
