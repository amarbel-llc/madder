package commands

import (
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("pack", &Pack{})
}

type Pack struct {
	command_components.EnvBlobStore
	command_components.BlobStore

	DeleteLoose      bool
	MaxPackSize      ui.HumanReadableBytes
	SkipMissingBlobs bool
	Delta            bool
}

var (
	_ interfaces.CommandComponentWriter = (*Pack)(nil)
	_ futility.CommandWithParams        = (*Pack)(nil)
)

func (cmd *Pack) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-ids",
			Description: "blob store IDs to pack (defaults to all packable stores)",
			Variadic:    true,
		},
	}
}

func (cmd Pack) GetDescription() futility.Description {
	return futility.Description{
		Short: "pack loose blobs into archive files",
		Long: "Consolidate loose blobs in inventory archive stores into " +
			"packed archive files for more efficient storage. With no " +
			"arguments, all packable stores are processed. Use " +
			"-delete-loose to remove loose blobs after successful " +
			"packing, -max-pack-size to control archive file size, " +
			"and -delta to enable delta compression.",
	}
}

func (cmd Pack) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	for id, blobStore := range blobStores {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *Pack) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(&cmd.DeleteLoose, "delete-loose", false,
		"validate archive then delete packed loose blobs")
	flagSet.BoolVar(&cmd.SkipMissingBlobs, "skip-missing-blobs", false,
		"skip unreadable loose blobs instead of aborting")
	flagSet.BoolVar(&cmd.Delta, "delta", false,
		"enable delta compression during packing")
	flagSet.Var(&cmd.MaxPackSize, "max-pack-size",
		"override max pack size (e.g. 100M, 1G, 0 = unlimited)",
	)
}

func (cmd Pack) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStoreMap := cmd.MakeBlobStoresFromIdsOrAll(req, envBlobStore)

	tw := tap.NewWriter(os.Stdout)

	for storeId, blobStore := range blobStoreMap {
		packable, ok := blobStore.BlobStore.(blob_stores.PackableArchive)
		if !ok {
			tw.Skip(storeId, "not packable")
			continue
		}

		if err := packable.Pack(blob_stores.PackOptions{
			Context:              req,
			DeleteLoose:          cmd.DeleteLoose,
			DeletionPrecondition: blob_stores.NopDeletionPrecondition(),
			MaxPackSize:          cmd.MaxPackSize.GetByteCount(),
			SkipMissingBlobs:     cmd.SkipMissingBlobs,
			Delta:                cmd.Delta,
			TapWriter:            tw,
		}); err != nil {
			tw.NotOk(
				fmt.Sprintf("pack %s", storeId),
				tap_diagnostics.FromError(err),
			)
			req.Cancel(err)
			return
		}

		tw.Ok(fmt.Sprintf("pack %s", storeId))
	}

	tw.Plan()
}
