package commands_cutting_garden

import (
	"fmt"
	"net/url"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/capture_receipt"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/madder/go/internal/hotel/cutting_garden_plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("restore", &Restore{
		EnvBlobStore: command_components.EnvBlobStore{BlobStoreXDGScope: "madder"},
	})
}

// Restore implements `cutting-garden restore <receipt-id> <dest>`
// per FDR 0001 (docs/features/0001-restore.md) and RFC 0003
// §Consumer Rules.
//
// Validates destination preconditions, parses the receipt, runs path
// sanitization across all entries, resolves the source store from
// the receipt's optional store-hint (or the -store override), then
// materializes per-type (file/dir/symlink/other).
type Restore struct {
	command_components.EnvBlobStore

	// Store is the value of the -store flag. When non-empty, it
	// overrides the receipt's store-hint resolution per FDR
	// §Store-Hint Resolution branch 1.
	Store string
}

var (
	_ interfaces.CommandComponentWriter = (*Restore)(nil)
	_ futility.CommandWithParams        = (*Restore)(nil)
)

func (cmd *Restore) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "receipt-id",
			Description: "markl-id of a cutting_garden-capture_receipt-fs-v1 blob",
		},
		futility.Arg[*values.String]{
			Name:        "dest",
			Description: "destination directory; MUST NOT exist at invocation time",
		},
	}
}

func (cmd *Restore) GetDescription() futility.Description {
	return futility.Description{
		Short: "restore a captured tree from a receipt blob",
		Long: "Materialize a directory tree previously captured by " +
			"`cutting-garden capture` into <dest>. The receipt is parsed, " +
			"each entry's destination path is validated against the " +
			"sanitization rules in RFC 0003 §Consumer Rules, and per-" +
			"type materialization writes files (streamed from their " +
			"blob), directories (created with the captured POSIX " +
			"mode), symlinks (with the literal captured target), and " +
			"skips entries of type 'other' with a notice. <dest> MUST " +
			"NOT exist at invocation time; the consumer creates it. " +
			"Refusal at any sanitization or precondition step happens " +
			"before any disk write.",
	}
}

func (cmd *Restore) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
}

func (cmd *Restore) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&cmd.Store,
		"store",
		"",
		"explicit blob-store-id to resolve receipt and entry blobs "+
			"against (overrides the receipt's store-hint resolution)",
	)
}

func (cmd *Restore) Run(req futility.Request) {
	receiptIdStr := req.PopArg("receipt-id")
	dest := req.PopArg("dest")
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	if err := cmd.runRestore(envBlobStore, receiptIdStr, dest); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
}

// runRestore validates preconditions, parses the receipt, dispatches
// to the restore plugin keyed by dest's URI scheme. Returns the first
// refusal or write failure as an error.
//
// Sanitization runs before any disk write per FDR §Sanitization: the
// entire receipt is refused atomically if any entry would escape.
// Materialization MUST NOT recover from a mid-stream blob read failure
// (FDR §Limitations: no rollback) — the destination is left partial
// in that case and the diagnostic names the failed entry. Both rules
// are enforced inside the plugin's Restore method.
func (cmd *Restore) runRestore(
	envBlobStore command_components.BlobStoreEnv,
	receiptIdStr string,
	destStr string,
) error {
	destURL, plugin, err := resolveRestorePlugin(destStr)
	if err != nil {
		return err
	}

	if err := plugin.ValidateDest(destURL, destStr); err != nil {
		return err
	}

	var receiptId markl.Id
	if err := receiptId.Set(receiptIdStr); err != nil {
		return errors.Wrapf(err, "parse receipt-id %q", receiptIdStr)
	}

	blob, typeTag, err := readReceiptBlob(envBlobStore, &receiptId, cmd.Store)
	if err != nil {
		return err
	}

	v1, ok := blob.(*capture_receipt.V1)
	if !ok {
		return errors.ErrorWithStackf(
			"receipt %s: unexpected blob shape %T (expected *V1)",
			&receiptId, blob)
	}

	// TODO(#NNN) cross-scheme restores. Today the receipt's
	// type-tag must match the dest plugin's TypeTag(): the file
	// plugin can only restore `cutting_garden-capture_receipt-fs-v1`
	// receipts. As more plugins arrive there will be legitimate
	// cross-scheme cases (e.g. mirroring an fs receipt into an s3
	// prefix). Revisit this guard then — likely as an explicit
	// `--allow-cross-scheme` flag or by letting RestorePlugin
	// declare which receipt tags it accepts.
	if typeTag.StringSansOp() != plugin.TypeTag() {
		return errors.ErrorWithStackf(
			"receipt %s: type-tag %q cannot be restored to scheme %q (plugin tag %q); cross-scheme restore is not supported",
			&receiptId, typeTag.StringSansOp(), destURL.Scheme, plugin.TypeTag(),
		)
	}

	materializationStore, err := resolveMaterializationStore(
		envBlobStore, v1.Hint, cmd.Store,
	)
	if err != nil {
		return err
	}

	return plugin.Restore(cutting_garden_plugins.RestoreRequest{
		Entries:   v1.Entries,
		BlobStore: materializationStore,
		Dest:      destURL,
		RawDest:   destStr,
	})
}

// resolveRestorePlugin parses destStr as a URL and looks up the
// restore plugin registered for its scheme. Schemeless dests resolve
// to the file plugin's `""` registration.
func resolveRestorePlugin(
	destStr string,
) (*url.URL, cutting_garden_plugins.RestorePlugin, error) {
	u, err := url.Parse(destStr)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "parse dest %q", destStr)
	}
	plugin, err := cutting_garden_plugins.ResolveRestore(u.Scheme)
	if err != nil {
		return nil, nil, err
	}
	return u, plugin, nil
}

// readReceiptBlob fetches and parses the receipt blob.
//
// Phase 1 of FDR §Store-Hint Resolution is bootstrapping: the hint
// isn't parsed yet, so -store is the only available signal. With
// -store unset, fall back across configured stores using HasBlob
// probes — mirrors cat.go's blobFromRemainingStores. The
// GetBlobStoresSorted ordering pins the search order so two stores
// holding receipts with colliding ids resolve deterministically.
func readReceiptBlob(
	envBlobStore command_components.BlobStoreEnv,
	receiptId *markl.Id,
	storeOverride string,
) (capture_receipt.Blob, ids.TypeStruct, error) {
	if storeOverride != "" {
		store, err := resolveStoreById(envBlobStore, storeOverride)
		if err != nil {
			return nil, ids.TypeStruct{}, err
		}
		blob, typeTag, err := capture_receipt.Read(store, receiptId)
		if err != nil {
			return nil, typeTag, errors.Wrapf(err, "read receipt %s", receiptId)
		}
		return blob, typeTag, nil
	}

	for _, store := range envBlobStore.GetBlobStoresSorted() {
		if !store.HasBlob(receiptId) {
			continue
		}
		blob, typeTag, err := capture_receipt.Read(store, receiptId)
		if err != nil {
			return nil, typeTag, errors.Wrapf(err, "read receipt %s", receiptId)
		}
		return blob, typeTag, nil
	}

	return nil, ids.TypeStruct{}, errors.ErrorWithStackf(
		"receipt %s not found in any configured store", receiptId)
}

// resolveMaterializationStore implements FDR §Store-Hint Resolution
// for entry materialization. -store wins; otherwise the hint dictates
// the store, with config-drift diagnostics on mismatch. A missing
// hint or missing/malformed/incompatible store falls back to the
// active default with a stderr notice — the FDR §Limitations
// §Hash-family mismatch carve-out widens that fallback path to
// include compute failures.
func resolveMaterializationStore(
	envBlobStore command_components.BlobStoreEnv,
	hint *capture_receipt.StoreHint,
	storeOverride string,
) (blob_stores.BlobStoreInitialized, error) {
	if storeOverride != "" {
		return resolveStoreById(envBlobStore, storeOverride)
	}

	if hint == nil {
		fmt.Fprintln(os.Stderr, "notice: receipt carries no store hint")
		fmt.Fprintln(os.Stderr, "notice: falling back to active store")
		return envBlobStore.GetDefaultBlobStore(), nil
	}

	var hintId blob_store_id.Id
	if err := hintId.Set(hint.StoreId); err != nil {
		fmt.Fprintf(os.Stderr,
			"notice: receipt store-hint id %q is malformed: %v\n",
			hint.StoreId, err)
		fmt.Fprintln(os.Stderr, "notice: falling back to active store")
		return envBlobStore.GetDefaultBlobStore(), nil
	}

	stores := envBlobStore.GetBlobStores()
	hintedStore, ok := stores[hintId.String()]
	if !ok {
		fmt.Fprintf(os.Stderr,
			"notice: receipt names store %q which is not configured locally\n",
			hint.StoreId)
		fmt.Fprintln(os.Stderr, "notice: falling back to active store")
		return envBlobStore.GetDefaultBlobStore(), nil
	}

	localHint, err := computeStoreHint(hintedStore, hintId)
	if err != nil || localHint == nil {
		fmt.Fprintf(os.Stderr,
			"notice: cannot compute local config-markl-id for store %q: %v\n",
			hint.StoreId, err)
		fmt.Fprintln(os.Stderr, "notice: falling back to active store")
		return envBlobStore.GetDefaultBlobStore(), nil
	}

	if localHint.ConfigMarklId == hint.ConfigMarklId {
		return hintedStore, nil
	}

	fmt.Fprintf(os.Stderr,
		"warning: store %s has been re-configured since this receipt was written\n"+
			"  receipt config-hash: %s\n"+
			"  current config-hash: %s\n",
		hint.StoreId, hint.ConfigMarklId, localHint.ConfigMarklId,
	)
	return blob_stores.BlobStoreInitialized{}, errors.ErrorWithStackf(
		"pass -store <id> to override and use the current store\n"+
			"hint: re-running with -store %s uses the current configuration",
		hint.StoreId,
	)
}

// resolveStoreById parses storeIdStr and looks up the corresponding
// configured store. Returns an error if the id is malformed or the
// store is not configured.
func resolveStoreById(
	envBlobStore command_components.BlobStoreEnv,
	storeIdStr string,
) (blob_stores.BlobStoreInitialized, error) {
	var storeId blob_store_id.Id
	if err := storeId.Set(storeIdStr); err != nil {
		return blob_stores.BlobStoreInitialized{},
			errors.Wrapf(err, "parse -store value %q", storeIdStr)
	}

	stores := envBlobStore.GetBlobStores()
	store, ok := stores[storeId.String()]
	if !ok {
		return blob_stores.BlobStoreInitialized{}, errors.ErrorWithStackf(
			"-store %q is not a configured blob store", storeIdStr,
		)
	}
	return store, nil
}

