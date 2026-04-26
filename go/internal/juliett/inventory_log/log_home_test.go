package inventory_log

import (
	"path/filepath"
	"testing"
)

// TestResolveLogHome_XDGLogHomeSet asserts that when $XDG_LOG_HOME is
// set and non-empty it is returned verbatim — highest precedence per
// xdg_log_home(7).
func TestResolveLogHome_XDGLogHomeSet(t *testing.T) {
	t.Setenv("XDG_LOG_HOME", "/tmp/custom-log")
	t.Setenv("HOME", "/home/irrelevant")

	if got := ResolveLogHome(); got != "/tmp/custom-log" {
		t.Errorf("ResolveLogHome() = %q, want %q", got, "/tmp/custom-log")
	}
}

// TestResolveLogHome_FallsBackToHome asserts the manpage default
// ($HOME/.local/log) when XDG_LOG_HOME is unset.
func TestResolveLogHome_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_LOG_HOME", "")
	t.Setenv("HOME", "/home/test")

	want := filepath.Join("/home/test", ".local", "log")
	if got := ResolveLogHome(); got != want {
		t.Errorf("ResolveLogHome() = %q, want %q", got, want)
	}
}

// TestMadderLogDir asserts the per-app subdir convention from
// xdg_log_home(7) NOTES is applied.
func TestMadderLogDir(t *testing.T) {
	t.Setenv("XDG_LOG_HOME", "/tmp/xlh")
	t.Setenv("HOME", "/home/irrelevant")

	want := filepath.Join("/tmp/xlh", "madder")
	if got := MadderLogDir(); got != want {
		t.Errorf("MadderLogDir() = %q, want %q", got, want)
	}
}
