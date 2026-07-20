package command_components

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/pool"
)

type Complete struct {
	EnvBlobStore
}

func (cmd Complete) GetFlagValueBlobIds(
	blobStoreId *scoped_id.Id,
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
