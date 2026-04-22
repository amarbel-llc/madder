package commands_cache

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/protocol"
)

func init() {
	utility.AddCmd("version", &Version{})
}

// Version prints the build version and commit SHA burnt in via -ldflags
// at build time. Kept in sync with cmd/madder's version command so both
// binaries self-report identically. Exposed as a tool over MCP with
// ReadOnlyHint set.
type Version struct{}

var (
	_ futility.CommandWithAnnotations = (*Version)(nil)
	_ futility.CommandWithParams      = (*Version)(nil)

	versionReadOnly = true
)

func (cmd Version) GetDescription() futility.Description {
	return futility.Description{
		Short: "print madder-cache build version and commit",
		Long: "Prints the version and commit SHA burnt in via -ldflags at " +
			"build time. Dev builds show \"dev+unknown\"; release builds " +
			"show \"X.Y.Z+abc1234\" from flake.nix and the git short SHA.",
	}
}

func (cmd *Version) GetParams() []futility.Param { return nil }

func (cmd *Version) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Version) GetAnnotations() *protocol.ToolAnnotations {
	return &protocol.ToolAnnotations{
		ReadOnlyHint: &versionReadOnly,
	}
}

func (cmd Version) Run(req futility.Request) {
	fmt.Fprintln(os.Stdout, buildinfo.String())
}
