package commands

// Globals is the utility-level value struct holding madder's global
// flag values. Accessed by lower packages (command_components) via
// structural typing — see inventoryLogFlagsReader in env_blob_store.go.
// That keeps command_components free of a circular import back into
// this package.
//
// A pointer to Globals is stored on Utility.GlobalFlags; the runtime
// chain is: GlobalFlagDefiner binds fields into flags.FlagSet →
// RunCLI parses the user's argv → the struct's fields are populated.
type Globals struct {
	// NoInventoryLog, when true, suppresses the per-blob audit
	// inventory-log under $XDG_LOG_HOME/madder/inventory_log/. Set via
	// the --no-inventory-log global flag. See ADR 0004 and the
	// docs/plans/2026-04-26-typed-write-log-design.md design.
	NoInventoryLog bool
}

// IsInventoryLogDisabled is the one-method surface command_components
// requires to decide whether to hand a NopObserver to env_dir.
func (g *Globals) IsInventoryLogDisabled() bool {
	if g == nil {
		return false
	}
	return g.NoInventoryLog
}
