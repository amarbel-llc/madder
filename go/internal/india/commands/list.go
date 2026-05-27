package commands

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
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
		Short: "list configured blob stores",
		Long: "List all blob stores configured for the current repository, " +
			"showing each store's ID, description, and the location of its " +
			"on-disk config file. In text mode each line is " +
			"'<id>: <description> # path: <rel>' where <rel> is the " +
			"config-file path expressed relative to the current working " +
			"directory (absolute fallback). In ndjson mode (-format=ndjson) " +
			"each store emits one JSON object per line with fields \"id\", " +
			"\"description\", \"config_path\" (absolute), and \"base\" " +
			"(absolute). In json mode (-format=json) the same per-store " +
			"records are emitted as values of a single top-level JSON " +
			"object keyed by store ID (the PWD-resolved form, e.g. " +
			"\".default\"). Output defaults to text on an interactive " +
			"terminal and to ndjson when stdout is piped; pass -format to " +
			"force a specific encoding. Store IDs use prefixes to indicate " +
			"scope: unprefixed for XDG user stores, '.' for CWD-relative " +
			"stores, and '/' for XDG system stores.",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

type listRecord struct {
	Id            string `json:"id"`
	Description   string `json:"description"`
	ConfigPath    string `json:"config_path"`
	Base          string `json:"base"`
	Digest        string `json:"digest,omitempty"`
	DigestMissing bool   `json:"digest_missing,omitempty"`
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
	var unmigrated []string

	for _, blobStore := range stableOrder(blobStores) {
		idStr := blobStore.Path.GetId().String()
		bd := blobStore.Config.BlobDigest
		if !bd.IsNull() {
			idStr += "@" + bd.String()
		} else {
			idStr += " (unmigrated)"
			unmigrated = append(unmigrated, blobStore.Path.GetId().String())
		}
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			idStr,
			blobStore.GetBlobStoreDescription(),
			envBlobStore.RelToCwdOrSame(blobStore.Path.GetConfig()),
		)
	}

	if len(unmigrated) > 0 {
		envBlobStore.GetUI().Printf("")
		envBlobStore.GetUI().Printf(
			"NOTE: %d store(s) above are missing tamper-detection digests.",
			len(unmigrated),
		)
		envBlobStore.GetUI().Printf("      Run this to migrate them:")
		envBlobStore.GetUI().Printf("")
		envBlobStore.GetUI().Printf(
			"        madder config-pin_digest %s",
			strings.Join(unmigrated, " "),
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
	rec := listRecord{
		Id:          blobStore.Path.GetId().String(),
		Description: blobStore.GetBlobStoreDescription(),
		ConfigPath:  blobStore.Path.GetConfig(),
		Base:        blobStore.Path.GetBase(),
	}
	bd := blobStore.Config.BlobDigest
	if !bd.IsNull() {
		rec.Digest = blob_store_configs.DigestPurpose + "@" + bd.String()
	} else {
		rec.DigestMissing = true
	}
	return rec
}
