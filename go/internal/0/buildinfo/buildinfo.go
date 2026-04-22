// Package buildinfo exposes the version and commit SHA that were burnt
// into the binary at build time via -ldflags. Nix builds override these
// values from flake.nix (madderVersion + self.shortRev); plain `go build`
// in the devshell leaves them at their defaults so local debugging is
// immediately distinguishable from an installed release.
//
// The `version` subcommand is the canonical consumer; any future code
// that needs to report build identity (e.g. a User-Agent header, an MCP
// initialize response, a crash-report footer) should also read from
// here rather than hardcoding a second string.
package buildinfo

// Version is the semver or "dev" for non-release builds. Overridden at
// link time via:
//
//	-X github.com/amarbel-llc/madder/go/internal/0/buildinfo.Version=<v>
var Version = "dev"

// Commit is the short git SHA or "unknown". Nix builds populate it from
// `self.shortRev or self.dirtyShortRev or "unknown"`, so a dirty local
// build shows `dirty-abcdef` rather than an undifferentiated "dev".
// Overridden at link time via:
//
//	-X github.com/amarbel-llc/madder/go/internal/0/buildinfo.Commit=<sha>
var Commit = "unknown"

// String returns "Version+Commit" — the canonical one-line build
// identity, matching moxy's convention (cmd/moxy/main.go).
func String() string {
	return Version + "+" + Commit
}
