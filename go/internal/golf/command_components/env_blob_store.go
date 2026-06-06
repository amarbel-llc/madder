package command_components

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/echo/madder_env"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_store_env"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/juliett/inventory_log"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/config_cli"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/debug"
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
// BlobStoreXDGScope, when non-empty, names the XDG scope used for blob
// store discovery — the `<scope>` segment in `$XDG_*_HOME/<scope>/
// blob_stores/`. Empty means use the calling utility's own name
// (req.Utility.GetName()). The field is nest-level agnostic: a command
// in a wrapper of a wrapper of madder still sets it to "madder" if
// that's where the blob stores ultimately live, regardless of how many
// layers of wrapping intervene. madder-mcp sets it to "madder" so the
// MCP server reads from madder's stores; madder and madder-cache leave
// it empty (each owns its own scope).
type EnvBlobStore struct {
	BlobStoreXDGScope string
}

func (cmd EnvBlobStore) MakeEnvBlobStore(
	req futility.Request,
) BlobStoreEnv {
	return blob_store_env.MakeBlobStoreEnv(cmd.makeEnvLocal(req))
}

// MakeEnvBlobStoreWithoutStores returns a BlobStoreEnv with the directory
// layout wired up but no blob stores discovered or initialized. Use this from
// commands that must operate before discovery would succeed, such as the
// legacy-config migration command.
func (cmd EnvBlobStore) MakeEnvBlobStoreWithoutStores(
	req futility.Request,
) BlobStoreEnv {
	return blob_store_env.MakeBlobStoreEnvWithoutStores(cmd.makeEnvLocal(req))
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

	xdgScope := cmd.BlobStoreXDGScope
	if xdgScope == "" {
		xdgScope = req.Utility.GetName()
	}

	envUI := env_ui.Make(
		req,
		config,
		debugOptions,
		envOptions,
	)

	dir := env_dir.MakeDefault(
		req,
		env_dir.Config{
			EnvVarNames:  madder_env.DefaultEnvVarNames,
			DebugOptions: debugOptions,
		},
		xdgScope,
	)

	(&dir).SetBlobWriteObserver(makeBlobWriteObserver(req))
	// Route env_dir's own chatter (dry-run delete notices) through the
	// env err sink so it honors CustomErr / UIFileIsStderr like the
	// blob-store and transfer chatter (#232).
	(&dir).SetUIErrPrinter(envUI.GetErr())

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
