package commands

import (
	"fmt"
	"sort"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("has", &Has{})
}

type Has struct {
	command_components.EnvBlobStore

	All bool
}

var (
	_ interfaces.CommandComponentWriter = (*Has)(nil)
	_ futility.CommandWithParams        = (*Has)(nil)
)

func (cmd *Has) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "args",
			Description: "markl IDs to check for existence, or blob-store-ids to scope subsequent checks",
			Variadic:    true,
		},
	}
}

func (cmd *Has) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(
		&cmd.All,
		"all",
		false,
		"list every store containing each blob (one tab-separated line per store); "+
			"incompatible with an explicit blob-store-id arg",
	)
}

func (cmd Has) GetDescription() futility.Description {
	return futility.Description{
		Short: "check if blobs exist",
		Long: "Check whether one or more blobs exist in the active blob " +
			"store. Arguments are markl IDs (e.g. blake2b256-...) or " +
			"blob-store-ids that switch the active store for subsequent " +
			"checks. Blob-store-ids support optional prefixes that " +
			"select the XDG scope ('.', '/', '%', '_', or none) — see " +
			"blob-store(7). With no blob-store-id prefix set, checks " +
			"fall back to searching every configured store if the blob " +
			"is missing from the active one. Exits 0 if all blobs are " +
			"found, nonzero if any are missing. For each found ID, " +
			"prints '<digest>\\tfound\\t<store-id>' for the store that " +
			"answered; missing IDs print '<digest>\\tnot found'. With " +
			"-all (no explicit blob-store-id), every configured store " +
			"that holds a given blob produces its own line.",
	}
}

func (cmd Has) Run(req futility.Request) {
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
			if cmd.All {
				errors.ContextCancelWithBadRequestf(
					req,
					"-all is incompatible with an explicit blob-store-id arg %q",
					arg,
				)
				return
			}

			blobStoreId = resolved.BlobStoreId
			explicitStore = true
			ui.Err().Print(arg_resolver.FormatStoreSwitchNotice(blobStoreId))
			continue

		case arg_resolver.KindError:
			ui.Err().Print(resolved.Err)
			missCount++
			continue
		}

		// KindBlobId
		stores := cmd.findStores(envBlobStore, &resolved.BlobId, blobStoreId, explicitStore)
		if len(stores) == 0 {
			envBlobStore.GetUI().Printf("%s\tnot found", &resolved.BlobId)
			missCount++
			continue
		}

		for _, id := range stores {
			envBlobStore.GetUI().Printf("%s\tfound\t%s", &resolved.BlobId, id)
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

func (cmd Has) findStores(
	envBlobStore command_components.BlobStoreEnv,
	blobId domain_interfaces.MarklId,
	blobStoreId blob_store_id.Id,
	explicitStore bool,
) []blob_store_id.Id {
	if explicitStore {
		store := envBlobStore.GetBlobStore(blobStoreId)
		if store.HasBlob(blobId) {
			return []blob_store_id.Id{store.Path.GetId()}
		}
		return nil
	}

	defaultStore, remaining := envBlobStore.GetDefaultBlobStoreAndRemaining()

	var hits []blob_store_id.Id

	if defaultStore.HasBlob(blobId) {
		hits = append(hits, defaultStore.Path.GetId())
		if !cmd.All {
			return hits
		}
	}

	for _, store := range stableOrder(remaining) {
		if store.HasBlob(blobId) {
			hits = append(hits, store.Path.GetId())
			if !cmd.All {
				return hits
			}
		}
	}

	return hits
}

// stableOrder sorts by id so -all output is deterministic across runs.
func stableOrder(m blob_stores.BlobStoreMap) []blob_stores.BlobStoreInitialized {
	out := make([]blob_stores.BlobStoreInitialized, 0, len(m))
	for _, store := range m {
		out = append(out, store)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Path.GetId().String() < out[j].Path.GetId().String()
	})

	return out
}
