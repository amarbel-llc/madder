package commands

import (
	"fmt"
	"os"
	"time"

	"code.linenisgreat.com/madder/go/internal/bravo/registry"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	tap "code.linenisgreat.com/tap/go/pkgs/writer"
)

func init() {
	utility.AddCmd("registry-gc", &RegistryGC{
		Retention: registry.DefaultRetention().String(),
	})
}

// RegistryGC prunes dangling entries from the per-host blob store registry
// index (dodder RFC-0007). A dangling entry is a symlink whose target
// blob_store-config no longer exists — the store was deleted or moved out from
// under the index. Entries younger than -retention (measured from registration
// time) are kept as a grace window against transient unavailability. v1 keeps
// no tombstone: a pruned entry leaves nothing behind.
type RegistryGC struct {
	// Retention is a Go duration string (e.g. "720h", "30m"). "0" is a no-op
	// that reports what a real GC would leave untouched.
	Retention string
}

var (
	_ interfaces.CommandComponentWriter = (*RegistryGC)(nil)
	_ futility.CommandWithParams        = (*RegistryGC)(nil)
)

func (cmd *RegistryGC) GetParams() []futility.Param { return nil }

func (cmd RegistryGC) GetDescription() futility.Description {
	return futility.Description{
		Short: "prune stale entries from the per-host blob store registry",
		Long: "Remove dangling entries from the per-host registry index at " +
			"$XDG_STATE_HOME/madder/index — symlinks whose target " +
			"blob_store-config no longer exists because the store was deleted " +
			"or moved. Only entries older than -retention (a Go duration, " +
			"default 720h / 30 days, measured from registration time) are " +
			"pruned; younger dangling entries are kept as a grace window " +
			"against a transiently-unavailable store (e.g. an unmounted " +
			"filesystem). Live entries are never touched. -retention=0 is a " +
			"no-op. The index is advisory: it feeds `madder list -all` only, " +
			"never blob-store resolution.",
	}
}

func (cmd *RegistryGC) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&cmd.Retention,
		"retention",
		cmd.Retention,
		"prune dangling entries older than this Go duration (e.g. \"720h\"); "+
			"\"0\" is a no-op",
	)
}

func (cmd *RegistryGC) Run(req futility.Request) {
	req.AssertNoMoreArgs()

	retention, err := time.ParseDuration(cmd.Retention)
	if err != nil {
		errors.ContextCancelWithBadRequestf(
			req,
			"invalid -retention %q: %s",
			cmd.Retention,
			err,
		)
		return
	}

	removed, err := registry.GC(retention)
	if err != nil {
		req.Cancel(err)
		return
	}

	tw := tap.NewWriter(os.Stdout)
	tw.Ok(fmt.Sprintf(
		"registry-gc pruned %d stale %s (retention %s)",
		removed,
		pluralEntries(removed),
		retention,
	))
	tw.Plan()
}

func pluralEntries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}
