package commands_cache

import (
	"bufio"
	"encoding/json"
	"os"

	"code.linenisgreat.com/madder/go/internal/charlie/output_format"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

func init() {
	utility.AddCmd("list", &List{Format: output_format.Default})
}

type List struct {
	command_components.EnvBlobStore

	Format output_format.Format
}

var (
	_ interfaces.CommandComponentWriter = (*List)(nil)
	_ futility.CommandWithParams        = (*List)(nil)
)

func (cmd *List) GetParams() []futility.Param { return nil }

func (cmd List) GetDescription() futility.Description {
	return futility.Description{
		Short: "list configured cache blob stores",
		Long: "List all cache blob stores configured for the current " +
			"environment, showing each store's ID, description, and the " +
			"location of its on-disk config file. In text mode each line " +
			"is '<id>: <description> # path: <rel>' where <rel> is the " +
			"config-file path expressed relative to the current working " +
			"directory (absolute fallback). In ndjson mode (-format=ndjson) " +
			"each store emits one JSON object per line with fields \"id\", " +
			"\"description\", \"config_path\" (absolute), and \"base\" " +
			"(absolute). In json mode (-format=json) the same per-store " +
			"records are emitted as values of a single top-level JSON " +
			"object keyed by store ID. Output defaults to text on an " +
			"interactive terminal and to ndjson when stdout is piped; " +
			"pass -format to force a specific encoding.",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

type listRecord struct {
	Id          string `json:"id"`
	Description string `json:"description"`
	ConfigPath  string `json:"config_path"`
	Base        string `json:"base"`
}

func (cmd List) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	// list is not a streaming TAP producer: tap-mode and the
	// auto-on-TTY default both render the same human text. ndjson
	// emits one record per line; json wraps the same records in a
	// single top-level object keyed by store id.
	var err error
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		err = emitListJSONObject(blobStores)
	case output_format.FormatNDJSON:
		err = emitListNDJSON(blobStores)
	case output_format.FormatTAP:
		emitListText(envBlobStore, blobStores)
	default:
		emitListText(envBlobStore, blobStores)
	}
	if err != nil {
		req.Cancel(err)
	}
}

func emitListText(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	for _, blobStore := range stableOrder(blobStores) {
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			blobStore.Path.GetId(),
			blobStore.GetBlobStoreDescription(),
			envBlobStore.RelToCwdOrSame(blobStore.Path.GetConfig()),
		)
	}
}

func emitListNDJSON(blobStores blob_stores.BlobStoreMap) (err error) {
	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)

	for _, blobStore := range stableOrder(blobStores) {
		_ = enc.Encode(makeListRecord(blobStore))
	}
	return nil
}

func emitListJSONObject(blobStores blob_stores.BlobStoreMap) (err error) {
	out := make(map[string]listRecord, len(blobStores))

	for _, blobStore := range blobStores {
		id := blobStore.Path.GetId().String()
		out[id] = makeListRecord(blobStore)
	}

	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return nil
}

func makeListRecord(blobStore blob_stores.BlobStoreInitialized) listRecord {
	return listRecord{
		Id:          blobStore.Path.GetId().String(),
		Description: blobStore.GetBlobStoreDescription(),
		ConfigPath:  blobStore.Path.GetConfig(),
		Base:        blobStore.Path.GetBase(),
	}
}
