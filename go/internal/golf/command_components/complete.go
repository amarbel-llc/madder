package command_components

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/alfa/pool"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type Complete struct {
	EnvBlobStore
}

func (cmd Complete) GetFlagValueBlobIds(
	blobStoreId *blob_store_id.Id,
) interfaces.FlagValue {
	return futility.FlagValueCompleter{
		FlagValue: blobStoreId,
		FuncCompleter: func(
			req futility.Request,
			envLocalAny any,
			commandLine futility.CommandLineInput,
		) {
			envLocal := envLocalAny.(env_local.Env)
			envBlobStore := cmd.MakeEnvBlobStore(req)
			blobStoresAll := envBlobStore.GetBlobStores()

			bufferedWriter, repool := pool.GetBufferedWriter(
				envLocal.GetUIFile(),
			)
			defer repool()

			defer errors.ContextMustFlush(envLocal, bufferedWriter)

			for _, blobStore := range blobStoresAll {
				bufferedWriter.WriteString(blobStore.Path.GetId().String())
				bufferedWriter.WriteByte('\t')
				bufferedWriter.WriteString(blobStore.GetBlobStoreDescription())
				bufferedWriter.WriteByte('\n')
			}
		},
	}
}
