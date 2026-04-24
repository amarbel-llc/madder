package commands

// Globals is the utility-level value struct holding madder's global
// flag values. Accessed by lower packages (command_components) via
// structural typing — see writeLogFlagsReader in env_blob_store.go.
// That keeps command_components free of a circular import back into
// this package.
//
// A pointer to Globals is stored on Utility.GlobalFlags; the runtime
// chain is: GlobalFlagDefiner binds fields into flags.FlagSet →
// RunCLI parses the user's argv → the struct's fields are populated.
type Globals struct {
	// NoWriteLog, when true, suppresses the per-blob audit write-log
	// under $XDG_LOG_HOME/madder/. Set via the --no-write-log global
	// flag. See ADR 0004 and issue #44.
	NoWriteLog bool
}

// IsWriteLogDisabled is the one-method surface command_components
// requires to decide whether to hand a NopObserver to env_dir.
func (g *Globals) IsWriteLogDisabled() bool {
	if g == nil {
		return false
	}
	return g.NoWriteLog
}
