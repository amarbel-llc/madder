package commands

import (
	"sort"

	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("pack-list", &PackList{})
}

type PackList struct {
	command_components.EnvBlobStore
	command_components.BlobStore
}

var _ futility.CommandWithParams = (*PackList)(nil)

func (cmd *PackList) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-ids",
			Description: "blob store IDs to list packs from (defaults to all)",
			Variadic:    true,
		},
	}
}

func (cmd PackList) GetDescription() futility.Description {
	return futility.Description{
		Short: "list archive files in inventory archive stores",
		Long: "List the archive pack files in one or more inventory archive " +
			"stores, showing each archive's checksum and the number of " +
			"blobs it contains. With no arguments, lists archives from " +
			"all packable stores.",
	}
}

func (cmd PackList) Complete(
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

func (cmd PackList) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStoreMap := cmd.MakeBlobStoresFromIdsOrAll(req, envBlobStore)

	for _, blobStore := range blobStoreMap {
		archiveIndex, ok := blobStore.BlobStore.(blob_stores.ArchiveIndex)
		if !ok {
			continue
		}

		entries := archiveIndex.AllArchiveEntryChecksums()

		checksums := make([]string, 0, len(entries))
		for checksum := range entries {
			checksums = append(checksums, checksum)
		}
		sort.Strings(checksums)

		for _, checksum := range checksums {
			envBlobStore.GetUI().Printf(
				"%s: %d entries",
				checksum,
				len(entries[checksum]),
			)
		}
	}
}
