package commands_cache

import (
	"fmt"
	"sort"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
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
			Name:        "markl-ids",
			Description: "markl IDs to check for existence",
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
		"list every store containing each blob (one tab-separated line per store)",
	)
}

func (cmd Has) GetDescription() futility.Description {
	return futility.Description{
		Short: "check if blobs exist in cache stores",
		Long: "Check whether one or more blobs exist in any configured cache " +
			"blob store. Exits 0 if all blobs are found, nonzero if any are " +
			"missing. For each found ID, prints " +
			"'<digest>\\tfound\\t<store-id>' for the store that answered; " +
			"missing IDs print '<digest>\\tnot found'. With -all, every " +
			"configured cache store that holds a given blob produces its " +
			"own line.",
	}
}

func (cmd Has) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	var missCount int

	for _, arg := range req.PopArgs() {
		var blobId markl.Id

		if err := blobId.Set(arg); err != nil {
			ui.Err().Print(errors.Errorf("invalid markl ID: %s", arg))
			missCount++
			continue
		}

		stores := cmd.findStores(envBlobStore, &blobId)
		if len(stores) == 0 {
			envBlobStore.GetUI().Printf("%s\tnot found", blobId)
			missCount++
			continue
		}

		for _, id := range stores {
			envBlobStore.GetUI().Printf("%s\tfound\t%s", blobId, id)
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
	blobId *markl.Id,
) []scoped_id.Id {
	defaultStore, remaining := envBlobStore.GetDefaultBlobStoreAndRemaining()

	var hits []scoped_id.Id

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
