package commands

import (
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("pack-cat-ids", &PackCatIds{})
}

type PackCatIds struct {
	command_components.EnvBlobStore
}

var _ futility.CommandWithParams = (*PackCatIds)(nil)

func (cmd *PackCatIds) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "archive-checksums",
			Description: "archive checksums to filter (defaults to all archives)",
			Variadic:    true,
		},
	}
}

func (cmd PackCatIds) GetDescription() futility.Description {
	return futility.Description{
		Short: "list blob digests contained in archive files",
		Long: "Output the blob digests stored in inventory archive pack " +
			"files. With no arguments, lists digests from all archives " +
			"across all stores. Pass archive checksums to filter to " +
			"specific archive files.",
	}
}

func (cmd PackCatIds) Run(req futility.Request) {
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
	envBlobStore command_components.BlobStoreEnv,
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
