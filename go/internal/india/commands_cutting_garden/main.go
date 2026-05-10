package commands_cutting_garden

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

var utility = futility.NewUtility("cutting-garden", "filesystem-tree capture and restore over content-addressable blob stores")

// globalFlags is the singleton Globals struct whose fields hold the
// parsed values of every --flag declared on the utility (not on a
// specific subcommand). Owned here, pointer published as
// utility.GlobalFlags for downstream type-asserting consumers.
var globalFlags = &Globals{}

func init() {
	utility.GlobalFlags = globalFlags
	utility.GlobalParams = []futility.Param{
		futility.BoolFlag{
			Name: "no-inventory-log",
			Description: "Suppress the per-blob audit inventory-log " +
				"under $XDG_LOG_HOME/madder/inventory_log/. See ADR 0004.",
		},
	}
	utility.GlobalFlagDefiner = func(fs *flags.FlagSet) {
		fs.BoolVar(
			&globalFlags.NoInventoryLog,
			"no-inventory-log",
			false,
			"Suppress the per-blob audit inventory-log under "+
				"$XDG_LOG_HOME/madder/inventory_log/. See ADR 0004.",
		)
	}

	utility.Examples = append(utility.Examples,
		futility.Example{
			Description: "Capture a directory tree into the default blob store and parse the receipt id.",
			Command:     "id=$(cutting-garden capture ./project | jq -r '.id')",
		},
		futility.Example{
			Description: "Restore a captured tree to a fresh destination.",
			Command:     "cutting-garden restore \"$id\" ./restore",
		},
		futility.Example{
			Description: "Use the cg alias against a CWD-relative blob store.",
			Command:     "cg capture .archive ./project",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path: "$XDG_DATA_HOME/madder/blob_stores/",
			Description: "Root directory for unprefixed (XDG user) blob " +
				"stores cutting-garden reads from / writes to. Managed by " +
				"madder(1).",
		},
		futility.FilePath{
			Path: "$PWD/.madder/local/share/blob_stores/",
			Description: "Root directory for CWD-relative blob stores " +
				"(those addressed with a '.' prefix, e.g. '.archive'). " +
				"Managed by madder(1).",
		},
		futility.FilePath{
			Path: "$XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<id>.hyphence",
			Description: "Append-only hyphence-wrapped NDJSON record of " +
				"every blob publish, one file per write session. " +
				"cutting-garden writes through madder's blob store API and " +
				"emits inventory-log records the same way. Suppress with " +
				"--no-inventory-log or MADDER_INVENTORY_LOG=0.",
		},
		futility.FilePath{
			Path: "$XDG_STATE_HOME/cutting-garden/captures.log",
			Description: "Append-only NDJSON record of every receipt " +
				"`cutting-garden capture` produces, one line per " +
				"receipt: {ts, receipt_id, store_id, roots[]}. Lives " +
				"under cutting-garden's own XDG scope (distinct from " +
				"madder's blob-store scope) so the audit trail is " +
				"separable from the data. Best-effort: write errors " +
				"surface as a stderr notice but never fail the capture.",
		},
	)

	utility.EnvVars = append(utility.EnvVars,
		futility.EnvVar{
			Name: "MADDER_CEILING_DIRECTORIES",
			Description: "Colon-separated list of absolute directories above " +
				"which cutting-garden will not walk when searching the " +
				"current working directory upward for a .madder override " +
				"directory. Mirrors GIT_CEILING_DIRECTORIES.",
		},
		futility.EnvVar{
			Name: "MADDER_XDG_USER_LOCATION_ONLY",
			Description: "Set to \"1\" to disable the cwd walk-up that would " +
				"otherwise resolve XDG paths against an ancestor .madder/ " +
				"directory. With this flag set, cutting-garden uses standard " +
				"XDG resolution only ($XDG_DATA_HOME etc., defaulting to " +
				"$HOME/.local/share). Useful for embedders and test harnesses " +
				"that exec cutting-garden from a cwd whose path branch a " +
				"MADDER_CEILING_DIRECTORIES entry can't gate.",
		},
		futility.EnvVar{
			Name: "HOME",
			Description: "User home directory. Base for XDG default paths " +
				"when XDG_* vars are unset.",
		},
		futility.EnvVar{
			Name: "XDG_DATA_HOME",
			Description: "Base directory for XDG user blob stores. Defaults " +
				"to $HOME/.local/share. Stores live under " +
				"$XDG_DATA_HOME/madder/blob_stores/.",
		},
		futility.EnvVar{
			Name: "XDG_CACHE_HOME",
			Description: "Base directory for XDG cache blob stores " +
				"(those addressed with the % prefix). Defaults to " +
				"$HOME/.cache.",
		},
		futility.EnvVar{
			Name: "XDG_CONFIG_HOME",
			Description: "Base directory for per-user CLI configuration. " +
				"Defaults to $HOME/.config.",
		},
		futility.EnvVar{
			Name: "XDG_STATE_HOME",
			Description: "Base directory for per-user state. Defaults to " +
				"$HOME/.local/state.",
		},
		futility.EnvVar{
			Name: "XDG_LOG_HOME",
			Description: "Base directory for the per-blob audit " +
				"inventory-log. Defaults to $HOME/.local/log. See ADR 0004 " +
				"and xdg_log_home(7).",
		},
		futility.EnvVar{
			Name: "MADDER_INVENTORY_LOG",
			Description: "Set to \"0\" to suppress the per-blob audit " +
				"inventory-log. Equivalent to the --no-inventory-log " +
				"global flag.",
		},
	)
}

func GetUtility() *futility.Utility {
	return utility
}
