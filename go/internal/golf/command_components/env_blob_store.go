package command_components

import (
	"os"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/juliett/inventory_log"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/foxtrot/config_cli"
)

// inventoryLogFlagsReader is the minimal surface `command_components`
// needs from the utility's globals struct to decide whether to enable
// the audit inventory-log. Any utility whose `*Globals` satisfies this
// will work — see india/commands/globals.go for the concrete
// implementation. Declared here (structurally-typed) to avoid a cyclic
// import on india/commands.
type inventoryLogFlagsReader interface {
	IsInventoryLogDisabled() bool
}

// DefaultConfig is the CLI config used by madder commands.
// Set by commands.init before any command runs.
var DefaultConfig = config_cli.Default()

type EnvBlobStore struct{}

func (cmd EnvBlobStore) MakeEnvBlobStore(
	req futility.Request,
) BlobStoreEnv {
	return MakeBlobStoreEnv(cmd.makeEnvLocal(req))
}

// MakeEnvBlobStoreWithoutStores returns a BlobStoreEnv with the directory
// layout wired up but no blob stores discovered or initialized. Use this from
// commands that must operate before discovery would succeed, such as the
// legacy-config migration command.
func (cmd EnvBlobStore) MakeEnvBlobStoreWithoutStores(
	req futility.Request,
) BlobStoreEnv {
	return MakeBlobStoreEnvWithoutStores(cmd.makeEnvLocal(req))
}

func (cmd EnvBlobStore) makeEnvLocal(
	req futility.Request,
) env_local.Env {
	config := DefaultConfig

	var debugOptions debug.Options
	var envOptions env_ui.Options

	if config != nil {
		debugOptions = config.Debug
		envOptions.CustomOut = config.CustomOut
		envOptions.CustomErr = config.CustomErr
	}

	dir := env_dir.MakeDefault(
		req,
		req.Utility.GetName(),
		debugOptions,
	)

	(&dir).SetBlobWriteObserver(makeBlobWriteObserver(req))

	envUI := env_ui.Make(
		req,
		config,
		debugOptions,
		envOptions,
	)

	return env_local.Make(envUI, dir)
}

// makeBlobWriteObserver returns the observer to wire into env_dir for
// this command invocation. Resolves the disable contract:
//
//  1. MADDER_INVENTORY_LOG=0 (exactly "0") → no-op observer.
//  2. --no-inventory-log global flag, if the utility's GlobalFlags
//     satisfies inventoryLogFlagsReader and reports true → no-op
//     observer.
//  3. Otherwise → FileObserver rooted at
//     inventory_log.MadderInventoryLogDir(), with its Close registered
//     as an After hook on req.Context. The hook fires when
//     errCtx.Run() inside futility's wrapped.Run completes, flushing
//     hyphence's bufio.Writer and the file before RunCLI returns.
//     Without this, the body adapter goroutine would be killed mid-
//     stream on process exit and the trailing buffer would be lost.
//     See ADR 0004's superseded-by note and madder#75.
func makeBlobWriteObserver(
	req futility.Request,
) domain_interfaces.BlobWriteObserver {
	if strings.TrimSpace(os.Getenv("MADDER_INVENTORY_LOG")) == "0" {
		return inventory_log.NopObserver{}
	}

	if g, ok := req.Utility.GlobalFlags.(inventoryLogFlagsReader); ok {
		if g.IsInventoryLogDisabled() {
			return inventory_log.NopObserver{}
		}
	}

	obs := inventory_log.NewFileObserver(inventory_log.MadderInventoryLogDir())
	errors.ContextCloseAfter(req.Context, obs)
	return obs
}
