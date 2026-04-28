// Package buildinfo exposes the version and commit SHA that were burnt
// into each binary at build time. Values are owned by `package main` in
// each cmd/ binary (to match the fork's auto-injected
// -X main.version / -X main.commit ldflags), and pushed in here via
// Set() from each binary's init().
//
// The `version` subcommand is the canonical consumer; any future code
// that needs to report build identity (e.g. a User-Agent header, a
// crash-report footer) should also read from here rather than
// hardcoding a second string.
package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
)

// Set is called from each cmd/ binary's init() with the
// ldflag-injected main.version / main.commit values. Must run before any
// consumer reads Version / Commit.
func Set(v, c string) {
	Version = v
	Commit = c
}

// String returns "Version+Commit" — the canonical one-line build
// identity, matching moxy's convention (cmd/moxy/main.go).
func String() string {
	return Version + "+" + Commit
}
