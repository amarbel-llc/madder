package commands

import (
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("has", &Has{})
}

type Has struct {
	command_components.EnvBlobStore
}

var _ command.CommandWithParams = (*Has)(nil)

func (cmd *Has) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "args",
			Description: "markl IDs to check for existence, or blob-store-ids to scope subsequent checks",
			Variadic:    true,
		},
	}
}

func (cmd Has) GetDescription() command.Description {
	return command.Description{
		Short: "check if blobs exist",
		Long: "Check whether one or more blobs exist in the active blob " +
			"store. Arguments are markl IDs (e.g. blake2b256-...) or " +
			"blob-store-ids that switch the active store for subsequent " +
			"checks. Blob-store-ids support optional prefixes that " +
			"select the XDG scope ('.', '/', '%', '_', or none) — see " +
			"blob-store(7). With no blob-store-id prefix set, checks " +
			"fall back to searching every configured store if the blob " +
			"is missing from the active one. Exits 0 if all blobs are " +
			"found, nonzero if any are missing. For each ID, prints the " +
			"digest followed by 'found' or 'not found'.",
	}
}

func (cmd Has) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	var blobStoreId blob_store_id.Id
	explicitStore := false
	var missCount int

	for _, arg := range req.PopArgs() {
		resolved := arg_resolver.Resolve(
			arg,
			arg_resolver.ModeBlobId|arg_resolver.ModeStoreSwitch,
		)

		switch resolved.Kind {
		case arg_resolver.KindStoreSwitch:
			blobStoreId = resolved.BlobStoreId
			explicitStore = true
			ui.Err().Printf("switched to blob-store-id: %s", blobStoreId)
			continue

		case arg_resolver.KindError:
			ui.Err().Print(resolved.Err)
			missCount++
			continue
		}

		// KindBlobId
		found := cmd.hasBlob(envBlobStore, &resolved.BlobId, blobStoreId, explicitStore)
		if found {
			envBlobStore.GetUI().Printf("%s\tfound", &resolved.BlobId)
		} else {
			envBlobStore.GetUI().Printf("%s\tnot found", &resolved.BlobId)
			missCount++
		}
	}

	if missCount > 0 {
		errors.ContextCancelWithError(
			req,
			errors.MakeErrNotFoundString(
				fmt.Sprintf("%d blob(s) not found", missCount),
			),
		)
	}
}

// hasBlob checks for blob existence. When the caller has scoped to an
// explicit blob-store-id via a prior switch arg, the check is limited
// to that store. Otherwise it searches every configured store.
func (cmd Has) hasBlob(
	envBlobStore command_components.BlobStoreEnv,
	blobId domain_interfaces.MarklId,
	blobStoreId blob_store_id.Id,
	explicitStore bool,
) bool {
	if explicitStore {
		return envBlobStore.GetBlobStore(blobStoreId).HasBlob(blobId)
	}

	defaultStore, remaining := envBlobStore.GetDefaultBlobStoreAndRemaining()

	if defaultStore.HasBlob(blobId) {
		return true
	}

	for _, store := range remaining {
		if store.HasBlob(blobId) {
			return true
		}
	}

	return false
}
