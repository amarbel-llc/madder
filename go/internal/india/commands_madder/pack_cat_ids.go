package commands_madder

import (
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/hotel/command_components_madder"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("pack-cat-ids", &PackCatIds{})
}

type PackCatIds struct {
	command_components_madder.EnvBlobStore
}

var _ command.CommandWithParams = (*PackCatIds)(nil)

func (cmd *PackCatIds) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "archive-checksums",
			Description: "archive checksums to filter (defaults to all archives)",
			Variadic:    true,
		},
	}
}

func (cmd PackCatIds) GetDescription() command.Description {
	return command.Description{
		Short: "list blob digests contained in archive files",
		Long: "Output the blob digests stored in inventory archive pack " +
			"files. With no arguments, lists digests from all archives " +
			"across all stores. Pass archive checksums to filter to " +
			"specific archive files.",
	}
}

func (cmd PackCatIds) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStoreMap := envBlobStore.GetBlobStores()

	archiveFilter := make(map[string]struct{})
	for _, arg := range req.PopArgs() {
		archiveFilter[arg] = struct{}{}
	}

	for _, blobStore := range blobStoreMap {
		cmd.runOne(envBlobStore, blobStore, archiveFilter)
	}
}

func (cmd PackCatIds) runOne(
	envBlobStore command_components_madder.BlobStoreEnv,
	blobStore blob_stores.BlobStoreInitialized,
	archiveFilter map[string]struct{},
) {
	archiveIndex, ok := blobStore.BlobStore.(blob_stores.ArchiveIndex)
	if !ok {
		return
	}

	entries := archiveIndex.AllArchiveEntryChecksums()

	for checksum, blobIds := range entries {
		if len(archiveFilter) > 0 {
			if _, ok := archiveFilter[checksum]; !ok {
				continue
			}
		}

		for _, blobId := range blobIds {
			envBlobStore.GetUI().Print(blobId)
		}
	}
}
