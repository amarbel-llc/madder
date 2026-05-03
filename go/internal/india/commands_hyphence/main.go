package commands_hyphence

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

var utility = futility.NewUtility(
	"hyphence",
	"format-only inspection and re-emission of on-disk hyphence documents (RFC 0001)",
)

var globalFlags = &Globals{}

func init() {
	utility.GlobalFlags = globalFlags
	utility.GlobalParams = []futility.Param{
		futility.BoolFlag{
			Name:        "no-inventory-log",
			Description: "Suppress the per-blob audit inventory-log under $XDG_LOG_HOME/madder/inventory_log/. No-op for hyphence (which performs no blob writes); kept for cross-utility flag-set consistency.",
		},
	}
	utility.GlobalFlagDefiner = func(fs *flags.FlagSet) {
		fs.BoolVar(
			&globalFlags.NoInventoryLog,
			"no-inventory-log",
			false,
			"Suppress the per-blob audit inventory-log. No-op for hyphence.",
		)
	}

	utility.Examples = append(utility.Examples,
		futility.Example{
			Description: "Validate a capture-receipt file against RFC 0001.",
			Command:     "hyphence validate receipt.hyphence",
		},
		futility.Example{
			Description: "Print just the metadata section of a document.",
			Command:     "hyphence meta receipt.hyphence | grep '^!'",
		},
		futility.Example{
			Description: "Pipe the body of an inventory-log file through jq.",
			Command:     "hyphence body $XDG_LOG_HOME/madder/inventory_log/2026-05-03/log.hyphence | jq -r '.entry_path'",
		},
		futility.Example{
			Description: "Canonicalize an old document.",
			Command:     "hyphence format old.hyphence > canonical.hyphence",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path:        "<any hyphence document>",
			Description: "Plain-text RFC 0001 document on disk. hyphence reads from a file path or stdin (use '-' for stdin).",
		},
	)
}

func GetUtility() *futility.Utility {
	return utility
}
