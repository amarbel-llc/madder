package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/charlie/tree_capture_receipt"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("tree-restore", &TreeRestore{})
}

// TreeRestore implements `madder tree-restore <receipt-id> <dest>`
// per FDR 0001 (docs/features/0001-tree-restore.md) and RFC 0003
// §Consumer Rules.
//
// Phase A scope: parse args, validate destination preconditions, parse
// the receipt, run path sanitization across all entries. No
// materialization; phase B adds per-type write logic and phase C adds
// store-hint resolution.
type TreeRestore struct {
	command_components.EnvBlobStore
}

var (
	_ interfaces.CommandComponentWriter = (*TreeRestore)(nil)
	_ futility.CommandWithParams        = (*TreeRestore)(nil)
)

func (cmd *TreeRestore) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "receipt-id",
			Description: "markl-id of a madder-tree_capture-receipt-v1 blob",
		},
		futility.Arg[*values.String]{
			Name:        "dest",
			Description: "destination directory; MUST NOT exist at invocation time",
		},
	}
}

func (cmd TreeRestore) GetDescription() futility.Description {
	return futility.Description{
		Short: "restore a captured tree from a receipt blob",
		Long: "Materialize a directory tree previously captured by " +
			"`madder tree-capture` into <dest>. The receipt is parsed, " +
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

func (cmd TreeRestore) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
}

func (cmd *TreeRestore) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}

func (cmd TreeRestore) Run(req futility.Request) {
	receiptIdStr := req.PopArg("receipt-id")
	dest := req.PopArg("dest")
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	if err := cmd.runRestore(req, envBlobStore, receiptIdStr, dest); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
}

// runRestore performs the phase-A flow: precondition check, receipt
// parse, sanitization. Returns the first refusal as an error;
// successful return means phase A passed and phase B (materialization,
// pending) would proceed.
//
// Phase A side-effects: none. The destination is not created, the
// store is read-only, no blobs are opened beyond the receipt itself.
func (cmd TreeRestore) runRestore(
	req futility.Request,
	envBlobStore command_components.BlobStoreEnv,
	receiptIdStr string,
	dest string,
) error {
	if err := assertDestinationDoesNotExist(dest); err != nil {
		return err
	}

	receiptId, err := parseReceiptId(receiptIdStr)
	if err != nil {
		return err
	}

	blobStore := envBlobStore.GetDefaultBlobStore()

	v1, err := loadReceiptV1(blobStore, receiptId)
	if err != nil {
		return err
	}

	if err := validateEntries(v1.Entries, dest); err != nil {
		return err
	}

	// Phase B: materialize. Phase C: store-hint resolution.
	// For now, surface a notice so a user invoking phase A's binary
	// gets a clear signal that the command parses and validates but
	// doesn't yet write anything.
	fmt.Fprintf(os.Stderr,
		"notice: tree-restore phase A: %d entries validated; "+
			"materialization pending\n",
		len(v1.Entries))

	return nil
}

func assertDestinationDoesNotExist(dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return errors.ErrorWithStackf(
			"%s: destination already exists\n"+
				"hint: choose a destination that does not exist, or remove this one",
			dest,
		)
	} else if !os.IsNotExist(err) {
		return errors.Wrapf(err, "stat %q", dest)
	}
	return nil
}

func parseReceiptId(s string) (*markl.Id, error) {
	var id markl.Id
	if err := id.Set(s); err != nil {
		return nil, errors.Wrapf(err, "parse receipt-id %q", s)
	}
	return &id, nil
}

func loadReceiptV1(
	blobStore blob_stores.BlobStoreInitialized,
	receiptId *markl.Id,
) (*tree_capture_receipt.V1, error) {
	reader, err := blobStore.MakeBlobReader(receiptId)
	if err != nil {
		return nil, errors.Wrapf(err, "open receipt blob %s", receiptId)
	}
	defer errors.DeferredCloser(&err, reader)

	tb := &hyphence.TypedBlob[tree_capture_receipt.Blob]{}
	if _, err = tree_capture_receipt.Coder.DecodeFrom(tb, reader); err != nil {
		return nil, errors.Wrapf(err, "decode receipt blob %s", receiptId)
	}

	v1, ok := tb.Blob.(*tree_capture_receipt.V1)
	if !ok {
		return nil, errors.ErrorWithStackf(
			"receipt %s: unexpected blob shape %T (expected *V1)",
			receiptId, tb.Blob)
	}
	return v1, nil
}

// validateEntries runs the RFC 0003 §Consumer Rules §Path Sanitization
// checks across every entry. Returns the first failure as a refusal
// error — atomic per the FDR: any refusal aborts phase A before any
// disk write.
//
// The materialized path used for the boundary check is
// `filepath.Clean(filepath.Join(dest, e.root, e.path))`. The
// destination boundary is `filepath.Clean(dest)`.
func validateEntries(entries []tree_capture_receipt.EntryV1, dest string) error {
	cleanDest := filepath.Clean(dest)

	for i := range entries {
		e := entries[i]

		if e.Root == "" {
			return errors.ErrorWithStackf(
				"entry has empty root\n"+
					"  path: %s",
				e.Path,
			)
		}

		if strings.ContainsRune(e.Root, 0) || strings.ContainsRune(e.Path, 0) {
			return errors.ErrorWithStackf(
				"entry contains NUL byte\n"+
					"  root: %q\n"+
					"  path: %q",
				e.Root, e.Path,
			)
		}

		materialized := filepath.Clean(filepath.Join(cleanDest, e.Root, e.Path))

		if !pathConfinedTo(materialized, cleanDest) {
			return errors.ErrorWithStackf(
				"entry escapes destination\n"+
					"  root: %s\n"+
					"  path: %s\n"+
					"  materialized: %s\n"+
					"  destination: %s",
				e.Root, e.Path, materialized, cleanDest,
			)
		}
	}

	return nil
}

// pathConfinedTo reports whether materialized is equal to or a strict
// descendant of dest. Both inputs MUST already be filepath.Clean'd.
//
// Per FDR §Sanitization, the test is:
//
//	materialized == dest || strings.HasPrefix(materialized, dest + os.PathSeparator)
//
// Equality covers the case of a `{root: ".", path: "."}` dir entry
// (the captured tree's root); the prefix test confines everything
// else to under dest.
func pathConfinedTo(materialized, dest string) bool {
	if materialized == dest {
		return true
	}
	return strings.HasPrefix(materialized, dest+string(os.PathSeparator))
}
