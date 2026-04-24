package commands

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

var utility = futility.NewUtility("madder", "content-addressable blob store operations")

// globalFlags is the singleton Globals struct whose fields hold the
// parsed values of every --flag declared on the utility (not on a
// specific subcommand). Owned here, pointer published as
// utility.GlobalFlags for downstream type-asserting consumers.
var globalFlags = &Globals{}

func init() {
	utility.GlobalFlags = globalFlags
	utility.GlobalParams = []futility.Param{
		futility.BoolFlag{
			Name: "no-write-log",
			Description: "Suppress the per-blob audit write-log under " +
				"$XDG_LOG_HOME/madder/. See ADR 0004.",
		},
	}
	utility.GlobalFlagDefiner = func(fs *flags.FlagSet) {
		fs.BoolVar(
			&globalFlags.NoWriteLog,
			"no-write-log",
			false,
			"Suppress the per-blob audit write-log under "+
				"$XDG_LOG_HOME/madder/. See ADR 0004.",
		)
	}

	utility.Examples = append(utility.Examples,
		futility.Example{
			Description: "Initialize a default (XDG user) blob store.",
			Command:     "madder init default",
		},
		futility.Example{
			Description: "Write a file and retrieve it by digest.",
			Command:     "hash=$(madder write -format tap ./notes.md | awk '/^ok/ {print $4}')\nmadder cat \"$hash\"",
		},
		futility.Example{
			Description: "Stream bytes from stdin and parse the digest out of NDJSON.",
			Command:     "printf 'hello' | madder write -format json - | jq -r '.id'",
		},
		futility.Example{
			Description: "Initialize a CWD-relative store and copy all default-store blobs into it.",
			Command:     "madder init .archive\nmadder sync .default .archive",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path: "$XDG_DATA_HOME/madder/blob_stores/",
			Description: "Root directory for unprefixed (XDG user) blob " +
				"stores. Each store lives in a subdirectory containing a " +
				"blob_store-config file.",
		},
		futility.FilePath{
			Path: "$PWD/.madder/local/share/blob_stores/",
			Description: "Root directory for CWD-relative blob stores " +
				"(those addressed with a '.' prefix, e.g. '.archive').",
		},
		futility.FilePath{
			Path: "$XDG_CACHE_HOME/madder-cache/blob_stores/",
			Description: "Root directory for XDG cache blob stores " +
				"(those addressed with a '%' prefix). Managed by " +
				"madder-cache(1).",
		},
		futility.FilePath{
			Path: "<store-root>/blob_store-config",
			Description: "Per-store configuration file in hyphence format. " +
				"Specifies hash type, compression, encryption, and " +
				"store-type-specific fields.",
		},
	)

	utility.EnvVars = append(utility.EnvVars,
		futility.EnvVar{
			Name: "MADDER_CEILING_DIRECTORIES",
			Description: "Colon-separated list of absolute directories above " +
				"which madder will not walk when searching the current " +
				"working directory upward for a .madder override directory. " +
				"Mirrors GIT_CEILING_DIRECTORIES; useful for isolating test " +
				"runs so madder does not inherit configuration from ancestor " +
				"directories.",
		},
		futility.EnvVar{
			Name: "HOME",
			Description: "User home directory. Base for XDG default paths " +
				"when XDG_* vars are unset.",
		},
		futility.EnvVar{
			Name: "XDG_DATA_HOME",
			Description: "Base directory for XDG user blob stores (the " +
				"default location for unprefixed store IDs). Defaults to " +
				"$HOME/.local/share. Stores live under " +
				"$XDG_DATA_HOME/madder/blob_stores/.",
		},
		futility.EnvVar{
			Name: "XDG_CACHE_HOME",
			Description: "Base directory for XDG cache blob stores " +
				"(those addressed with the % prefix, e.g. '%scratch'). " +
				"Defaults to $HOME/.cache.",
		},
		futility.EnvVar{
			Name: "XDG_CONFIG_HOME",
			Description: "Base directory for per-user CLI configuration " +
				"honored by the underlying framework. Defaults to " +
				"$HOME/.config.",
		},
		futility.EnvVar{
			Name: "XDG_STATE_HOME",
			Description: "Base directory for per-user state honored by the " +
				"underlying framework. Defaults to $HOME/.local/state.",
		},
		futility.EnvVar{
			Name: "XDG_LOG_HOME",
			Description: "Base directory for the per-blob audit write-log. " +
				"Defaults to $HOME/.local/log. Logs live under " +
				"$XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson. See " +
				"ADR 0004 and xdg_log_home(7).",
		},
		futility.EnvVar{
			Name: "MADDER_WRITE_LOG",
			Description: "Set to \"0\" to suppress the per-blob audit " +
				"write-log. Equivalent to the --no-write-log global flag. " +
				"Any other value (including unset) leaves logging enabled.",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path: "$XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson",
			Description: "Append-only NDJSON record of every blob publish, " +
				"one file per calendar day. Deletion is safe and must not " +
				"affect application correctness (per xdg_log_home(7)). " +
				"Suppress with --no-write-log or MADDER_WRITE_LOG=0.",
		},
	)
}

func GetUtility() *futility.Utility {
	return utility
}
