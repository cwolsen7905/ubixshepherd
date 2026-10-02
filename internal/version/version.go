// Package version holds the build's version, set at link time:
//
//	go build -ldflags "-X github.com/ubixsys/ubixshepherd/internal/version.Version=v0.1.0"
package version

// Version is "dev" unless the build sets it.
var Version = "dev"
