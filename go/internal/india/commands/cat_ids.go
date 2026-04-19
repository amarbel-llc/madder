package commands

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/collections_slice"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("cat-ids", &CatIds{
		Format: markl.IdFormatDefault,
	})
}

type CatIds struct {
	Format markl.IdFormat

	command_components.EnvBlobStore
	command_components.BlobStore
}

var _ command.CommandWithParams = (*CatIds)(nil)

func (cmd *CatIds) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", "output format for blob ids")
}

func (cmd *CatIds) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "store-ids",
			Description: "blob store IDs to query (defaults to all)",
			Variadic:    true,
		},
	}
}

func (cmd CatIds) GetDescription() command.Description {
	return command.Description{
		Short: "list all blob digests in a store",
		Long: "Output every blob digest stored in one or more blob stores. " +
			"With no arguments, lists digests from all configured stores. " +
			"Pass store IDs to query specific stores. Store IDs support " +
			"optional prefixes that select the XDG scope ('.', '/', '%', " +
			"'_', or none) — see blob-store(7). Use -format to control " +
			"the output encoding of blob digests.",
	}
}

func (cmd CatIds) Complete(
	req command.Request,
	envLocal env_local.Env,
	commandLine command.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	// args := commandLine.FlagsOrArgs[1:]

	// if commandLine.InProgress != "" {
	// 	args = args[:len(args)-1]
	// }

	for id, blobStore := range blobStores {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd CatIds) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	blobStores := cmd.MakeBlobStoresFromIdsOrAll(req, envBlobStore)

	var blobErrors collections_slice.Slice[command_components.BlobError]

	for _, blobStore := range blobStores {
		cmd.runOne(envBlobStore, blobStore, &blobErrors)
	}

	command_components.PrintBlobErrors(envBlobStore, blobErrors)
}

func (cmd CatIds) runOne(
	envBlobStore command_components.BlobStoreEnv,
	blobStore blob_stores.BlobStoreInitialized,
	blobErrors *collections_slice.Slice[command_components.BlobError],
) {
	for id, err := range blobStore.AllBlobs() {
		errors.ContextContinueOrPanic(envBlobStore)

		if err != nil {
			blobErrors.Append(
				command_components.BlobError{BlobId: id, Err: err},
			)
		} else {
			envBlobStore.GetUI().Print(cmd.Format.FormatId(id))
		}
	}
}
