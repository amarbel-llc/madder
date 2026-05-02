package commands_cutting_garden

// Globals is the utility-level value struct holding cutting-garden's
// global flag values. Accessed by command_components via structural
// typing — see inventoryLogFlagsReader in env_blob_store.go.
type Globals struct {
	// NoInventoryLog, when true, suppresses the per-blob audit
	// inventory-log under $XDG_LOG_HOME/madder/inventory_log/. Set via
	// the --no-inventory-log global flag. See ADR 0004.
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
