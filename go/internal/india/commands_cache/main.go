package commands_cache

import "github.com/amarbel-llc/purse-first/libs/dewey/golf/command"

var utility = command.NewUtility(
	"madder-cache",
	"purgeable content-addressable blob store operations",
)

func init() {
	utility.Examples = append(utility.Examples,
		command.Example{
			Description: "Initialize a cache store under XDG_CACHE_HOME.",
			Command:     "madder-cache init scratch",
		},
		command.Example{
			Description: "Write a build artifact into the cache and retrieve it later.",
			Command:     "hash=$(madder-cache write -format json ./build.tar.gz | jq -r '.id')\nmadder-cache cat \"$hash\" > restored.tar.gz",
		},
	)

	utility.Files = append(utility.Files,
		command.FilePath{
			Path: "$XDG_CACHE_HOME/madder-cache/blob_stores/",
			Description: "Root directory for cache blob stores. Purgeable — " +
				"contents may be removed by the system or the user without " +
				"affecting non-cache madder state.",
		},
		command.FilePath{
			Path:        "<store-root>/blob_store-config",
			Description: "Per-store configuration file in hyphence format.",
		},
	)

	utility.EnvVars = append(utility.EnvVars,
		command.EnvVar{
			Name: "MADDER_CEILING_DIRECTORIES",
			Description: "Colon-separated list of absolute directories above " +
				"which madder-cache will not walk when searching the current " +
				"working directory upward for a .madder override directory. " +
				"Mirrors GIT_CEILING_DIRECTORIES; useful for isolating test " +
				"runs so madder-cache does not inherit configuration from " +
				"ancestor directories.",
		},
		command.EnvVar{
			Name: "HOME",
			Description: "User home directory. Base for XDG default paths " +
				"when XDG_* vars are unset.",
		},
		command.EnvVar{
			Name: "XDG_CACHE_HOME",
			Description: "Base directory for purgeable blob stores. Defaults " +
				"to $HOME/.cache. Stores live under " +
				"$XDG_CACHE_HOME/madder-cache/blob_stores/.",
		},
		command.EnvVar{
			Name: "XDG_DATA_HOME",
			Description: "Base directory for non-cache XDG stores referenced " +
				"via the unprefixed name or the ~ prefix. Defaults to " +
				"$HOME/.local/share.",
		},
		command.EnvVar{
			Name: "XDG_CONFIG_HOME",
			Description: "Base directory for per-user CLI configuration " +
				"honored by the underlying framework. Defaults to " +
				"$HOME/.config.",
		},
		command.EnvVar{
			Name: "XDG_STATE_HOME",
			Description: "Base directory for per-user state honored by the " +
				"underlying framework. Defaults to $HOME/.local/state.",
		},
	)
}

func GetUtility() *command.Utility {
	return utility
}
