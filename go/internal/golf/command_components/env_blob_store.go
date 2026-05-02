package command_components

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/juliett/inventory_log"
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

// EnvBlobStore is the env-construction mixin embedded by every command
// that operates against a blob store.
//
// BlobStoreParentUtility, when non-empty, is the XDG utility name used
// for blob store discovery instead of req.Utility.GetName(). Set this
// when the calling utility is a *child* that consumes another utility's
// blob stores: for example, cutting-garden sets it to "madder" so it
// reads/writes madder's `$XDG_*_HOME/madder/blob_stores/` rather than
// carving out a parallel cutting-garden-named namespace that would
// never be populated. madder and madder-cache leave it empty, since
// each owns its own namespace.
type EnvBlobStore struct {
	BlobStoreParentUtility string
}

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

	xdgUtilityName := cmd.BlobStoreParentUtility
	if xdgUtilityName == "" {
		xdgUtilityName = req.Utility.GetName()
	}

	dir := env_dir.MakeDefault(
		req,
		xdgUtilityName,
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
// this command invocation. The CLI-only --no-inventory-log flag
// short-circuits to a NopObserver here; the env-var disable
// (MADDER_INVENTORY_LOG=0) and the FileObserver+ContextCloseAfter
// wiring are handled by inventory_log.WireDefault, which library
// importers also use (#76).
//
// Both *FileObserver and NopObserver satisfy domain_interfaces
// .BlobWriteObserver directly, so we type-assert rather than wrap
// with AsBlobWriteObserver — wrapping would hide *FileObserver's
// DescriptionSetter capability from the write command's type
// assertion (write.go:134).
func makeBlobWriteObserver(
	req futility.Request,
) domain_interfaces.BlobWriteObserver {
	if g, ok := req.Utility.GlobalFlags.(inventoryLogFlagsReader); ok {
		if g.IsInventoryLogDisabled() {
			return inventory_log.NopObserver{}
		}
	}

	return inventory_log.WireDefault(req.Context).(domain_interfaces.BlobWriteObserver)
}
