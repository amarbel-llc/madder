package commands

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
)

var utility = futility.NewUtility("madder", "content-addressable blob store operations")

func init() {
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
	)
}

func GetUtility() *futility.Utility {
	return utility
}
