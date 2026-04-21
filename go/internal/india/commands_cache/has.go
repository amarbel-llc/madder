package commands_cache

import (
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("has", &Has{})
}

type Has struct {
	command_components.EnvBlobStore
}

var _ futility.CommandWithParams = (*Has)(nil)

func (cmd *Has) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "markl-ids",
			Description: "markl IDs to check for existence",
			Variadic:    true,
		},
	}
}

func (cmd Has) GetDescription() futility.Description {
	return futility.Description{
		Short: "check if blobs exist in cache stores",
		Long: "Check whether one or more blobs exist in any configured cache " +
			"blob store. Exits 0 if all blobs are found, nonzero if any are " +
			"missing. For each ID, prints the digest followed by 'found' " +
			"or 'not found'.",
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

		if cmd.hasInAnyStore(envBlobStore, &blobId) {
			envBlobStore.GetUI().Printf("%s\tfound", blobId)
		} else {
			envBlobStore.GetUI().Printf("%s\tnot found", blobId)
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

func (cmd Has) hasInAnyStore(
	envBlobStore command_components.BlobStoreEnv,
	blobId *markl.Id,
) bool {
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
