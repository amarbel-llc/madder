package commands_mcp

import "github.com/amarbel-llc/madder/go/internal/futility"

var utility = futility.NewUtility(
	"madder-mcp",
	"MCP server exposing madder blobs as resources over stdio",
)

func init() {
	utility.Examples = append(utility.Examples,
		futility.Example{
			Description: "Run the MCP server (used as a clown stdioServer command).",
			Command:     "madder-mcp serve",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path: "$XDG_DATA_HOME/madder/blob_stores/",
			Description: "Root directory for the madder XDG blob stores read " +
				"by the MCP server. madder-mcp pins its blob-store XDG " +
				"scope to \"madder\" so it sees the same stores as the " +
				"madder(1) CLI, regardless of its own utility name.",
		},
	)

	utility.EnvVars = append(utility.EnvVars,
		futility.EnvVar{
			Name: "MADDER_CEILING_DIRECTORIES",
			Description: "Colon-separated list of absolute directories above " +
				"which madder-mcp will not walk when searching the current " +
				"working directory upward for a .madder override directory. " +
				"Mirrors GIT_CEILING_DIRECTORIES.",
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
			Description: "Base directory for cache blob stores. Defaults to " +
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
	)
}

func GetUtility() *futility.Utility {
	return utility
}
