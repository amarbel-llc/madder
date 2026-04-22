package commands_cache

import (
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func init() {
	utility.AddCmd("init", &Init{
		tipe: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		blobStoreConfig: &blob_store_configs.DefaultType{
			HashTypeId:      blob_store_configs.HashTypeDefault,
			HashBuckets:     blob_store_configs.DefaultHashBuckets,
			CompressionType: compression_type.CompressionTypeDefault,
		},
	})
}

type Init struct {
	tipe            ids.TypeStruct
	blobStoreConfig blob_store_configs.ConfigMutable

	command_components.EnvBlobStore
	command_components.Init
}

var (
	_ interfaces.CommandComponentWriter = (*Init)(nil)
	_ futility.CommandWithParams        = (*Init)(nil)
)

func (cmd *Init) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-id",
			Description: "identifier for the new blob store (e.g. '%default')",
			Required:    true,
		},
	}
}

func (cmd Init) GetDescription() futility.Description {
	return futility.Description{
		Short: "initialize a purgeable blob store",
		Long: "Create a new local content-addressable blob store under " +
			"XDG_CACHE_HOME with hash-bucketed directory layout. " +
			"Cache stores can be safely purged without data loss.",
	}
}

func (cmd *Init) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	cmd.blobStoreConfig.SetFlagDefinitions(flagDefinitions)
}

func (cmd *Init) Run(req futility.Request) {
	var blobStoreId blob_store_id.Id

	if err := blobStoreId.Set(req.PopArg("blob-store-id")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	req.AssertNoMoreArgs()

	tw := tap.NewWriter(os.Stdout)

	envBlobStore := cmd.MakeEnvBlobStore(req)

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&blob_store_configs.TypedConfig{
			Type: cmd.tipe,
			Blob: cmd.blobStoreConfig,
		},
	)

	tw.Ok(fmt.Sprintf("init %s", pathConfig.GetConfig()))
	tw.Plan()
}
