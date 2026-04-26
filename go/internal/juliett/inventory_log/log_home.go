package inventory_log

import (
	"os"
	"path/filepath"
)

// ResolveLogHome returns the user's $XDG_LOG_HOME directory per the
// xdg_log_home(7) extension, falling back to $HOME/.local/log when
// unset or empty. If $HOME is also unset (unusual), falls back to
// ".local/log" relative to the cwd so callers never receive an empty
// string.
func ResolveLogHome() string {
	if v := os.Getenv("XDG_LOG_HOME"); v != "" {
		return v
	}

	home := os.Getenv("HOME")
	if home == "" {
		return filepath.Join(".local", "log")
	}

	return filepath.Join(home, ".local", "log")
}

// MadderLogDir returns the madder-scoped subdirectory of $XDG_LOG_HOME.
// Apps should namespace their logs per xdg_log_home(7) NOTES.
func MadderLogDir() string {
	return filepath.Join(ResolveLogHome(), "madder")
}

// MadderInventoryLogDir returns the inventory-log root directory:
// $XDG_LOG_HOME/madder/inventory_log/. FileObserver creates per-day
// subdirectories under this path.
func MadderInventoryLogDir() string {
	return filepath.Join(MadderLogDir(), "inventory_log")
}
