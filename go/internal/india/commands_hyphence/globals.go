package commands_hyphence

// Globals carries hyphence's global flag values. The --no-inventory-log
// flag is mounted for cross-utility consistency but has no effect on
// hyphence's operation since hyphence performs no blob writes.
type Globals struct {
	NoInventoryLog bool
}

func (g *Globals) IsInventoryLogDisabled() bool {
	if g == nil {
		return false
	}
	return g.NoInventoryLog
}
