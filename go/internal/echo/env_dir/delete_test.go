//go:build test

package env_dir

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/debug"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
)

// makeDryRunEnvAt builds a madder-scoped env_dir with DryRun set,
// sandboxing HOME and the XDG vars under temp dirs so construction's
// initializeXDG never touches the real home (unwritable in the nix
// build sandbox, polluted otherwise).
func makeDryRunEnvAt(t *testing.T) env {
	t.Helper()

	sandbox := t.TempDir()
	t.Setenv("HOME", filepath.Join(sandbox, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(sandbox, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(sandbox, "state"))
	t.Setenv("XDG_RUNTIME_HOME", filepath.Join(sandbox, "runtime"))
	t.Setenv("MADDER_CEILING_DIRECTORIES", sandbox)

	cwd := filepath.Join(sandbox, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	saved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })

	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir %q: %v", cwd, err)
	}

	return MakeDefault(
		errors.MakeContextDefault(),
		Config{DebugOptions: debug.Options{DryRun: true}},
		"madder",
	)
}

// TestDelete_DryRunChatterRoutesToConfiguredPrinter pins #232: the
// dry-run "would delete" notice must route through the env's
// configured err printer (wired at construction time like the
// blob-write observer) so library consumers — dodder calls Delete from
// its checkout/deinit flows — can redirect it. With no printer
// configured the notice falls back to the process-global stderr
// printer, unchanged from before.
func TestDelete_DryRunChatterRoutesToConfiguredPrinter(t *testing.T) {
	env := makeDryRunEnvAt(t)

	var buf bytes.Buffer
	(&env).SetUIErrPrinter(ui.MakePrinterFromWriter(&buf))

	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := env.Delete(target); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "would delete:") {
		t.Fatalf(
			"dry-run notice did not reach the configured printer; buffer: %q",
			out,
		)
	}

	// Dry-run must not have removed the file.
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("dry-run Delete removed the target: %v", err)
	}
}
