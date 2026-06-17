package env_dir

import "github.com/amarbel-llc/purse-first/libs/dewey/pkgs/debug"

// EnvVarNames bundles the env var names env_dir reads from and writes to.
// Consumers supply their own bundle via Config.EnvVarNames; env_dir does
// not provide application-specific defaults — that belongs at the
// caller's application layer (e.g. madder commands pass
// `madder_env.DefaultEnvVarNames`).
//
// Empty fields opt the corresponding env-var feature OUT entirely:
//
//   - Empty `Binary`: env_dir does not publish the binary path to
//     subprocesses (AddToEnvVars and MakeCommonEnv emit nothing).
//   - Empty `XDGUtilityOverride`: env_dir does not honor any env-var
//     override of the XDG scope (the dewey XDG stack receives an
//     empty override-env-var-name and therefore reads no override).
//   - Empty `VerifyOnCollision`: env_dir does not honor any env-var
//     override of the verify-on-collision toggle
//     (`os.Getenv("")` returns "", which parses as false).
//   - Empty `XDGUserLocationOnly`: env_dir does not honor any env-var
//     opt-out of the cwd walk-up (MakeWithDefaultHome's
//     permitCwdXDGOverride arg is the only knob).
//
// This makes `Config{}` (zero-value) a viable construction shape for
// callers that want a pure XDG-only env_dir with no env-var-driven
// behavior.
type EnvVarNames struct {
	// Binary names the env var written for subprocesses so they can find
	// the binary that spawned them.
	Binary string

	// XDGUtilityOverride names the env var the dewey XDG stack consults
	// to override the utilityName the call site passes in.
	XDGUtilityOverride string

	// VerifyOnCollision names the env var that, when truthy, flips
	// GetVerifyOnCollisionOverride to true. See ADR 0003 for rationale,
	// #38 for the eventual migration to a CLI flag.
	VerifyOnCollision string

	// XDGUserLocationOnly names the env var that, when truthy (parseBoolEnv:
	// "1", "true", "yes", "on", case-insensitive), disables the cwd walk-up
	// MakeDefault would otherwise perform via
	// xdg.InitializeOverriddenIfNecessary. With this set, env_dir falls
	// through to xdg.InitializeStandardFromEnv — honoring $XDG_DATA_HOME
	// etc. directly. Useful for embedders and test harnesses that exec
	// madder from a cwd whose path branch the ceiling can't gate.
	XDGUserLocationOnly string
}

// Config bundles the construction inputs that are reusable across XDG
// scopes. The XDG scope itself is NOT here — it is passed as its own arg
// to each Make* constructor (or implied by an externally-supplied
// xdg.XDG / dotenv path) so a single Config value can be applied to
// multiple env_dir instances with disjoint scopes:
//
//	cfg := env_dir.Config{
//	    EnvVarNames:  madder_env.DefaultEnvVarNames,
//	    DebugOptions: dbg,
//	}
//	madderEnv := env_dir.MakeDefault(ctx, cfg, "madder")
//	cgEnv     := env_dir.MakeDefault(ctx, cfg, "cutting-garden")
//
// EnvVarNames is taken as-is. env_dir does NOT supply defaults — see
// the EnvVarNames doc-comment for what empty fields mean.
type Config struct {
	EnvVarNames  EnvVarNames
	DebugOptions debug.Options

	// RepoName, when non-empty, nests this env's blob-store XDG under
	// repos/<RepoName>/ so a named FDR-0019 repo gets an isolated blob
	// pool (madder#240). Empty means the shared, un-nested layout. Only
	// the blob-store XDG is affected; metadata XDG nesting (if any) is
	// the caller's concern.
	RepoName string

	// SystemRoot is the filesystem root an XDG-system (`//name`) blob
	// store resolves under (madder#230) — its category dirs are rooted
	// here by rootAtSystem, so a system store lands at
	// <SystemRoot>/blob_stores/<name>. Injected by the caller (env_dir
	// stays application-agnostic for its eventual move to dewey); the
	// madder layer passes madder_env.DefaultSystemRoot (/var/lib/madder).
	// Empty disables system-scope resolution. Tests inject a sandbox dir.
	SystemRoot string

	// SystemScoped, when true (with SystemRoot set), roots this env's BASE
	// XDG — and therefore its per-pid TempLocal — under SystemRoot at
	// construction (madder#230 increment). A plain system store's blob
	// path is already system-rooted per-id via GetXDGForBlobStoreId; this
	// additionally colocates the link(2) staging temp under SystemRoot so
	// writes to a system store don't cross filesystems (EXDEV) and a
	// hardened systemd unit (ProtectSystem=strict, ProtectHome) doesn't
	// fail temp creation. Set by `madder serve --store //name`.
	SystemScoped bool
}
