package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
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

func (cmd *TreeRestore) GetDescription() futility.Description {
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

func (cmd *TreeRestore) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
}

func (cmd *TreeRestore) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}

func (cmd *TreeRestore) Run(req futility.Request) {
	receiptIdStr := req.PopArg("receipt-id")
	dest := req.PopArg("dest")
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	if err := cmd.runRestore(envBlobStore, receiptIdStr, dest); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
}

// runRestore validates preconditions, parses the receipt, runs path
// sanitization across every entry, then materializes per-type. Returns
// the first refusal or write failure as an error.
//
// Sanitization runs before any disk write per FDR §Sanitization: the
// entire receipt is refused atomically if any entry would escape.
// Materialization MUST NOT recover from a mid-stream blob read failure
// (FDR §Limitations: no rollback) — the destination is left partial
// in that case and the diagnostic names the failed entry.
func (cmd *TreeRestore) runRestore(
	envBlobStore command_components.BlobStoreEnv,
	receiptIdStr string,
	dest string,
) error {
	if err := assertDestinationDoesNotExist(dest); err != nil {
		return err
	}

	var receiptId markl.Id
	if err := receiptId.Set(receiptIdStr); err != nil {
		return errors.Wrapf(err, "parse receipt-id %q", receiptIdStr)
	}

	blobStore := envBlobStore.GetDefaultBlobStore()

	blob, _, err := tree_capture_receipt.Read(blobStore, &receiptId)
	if err != nil {
		return errors.Wrapf(err, "read receipt %s", &receiptId)
	}

	v1, ok := blob.(*tree_capture_receipt.V1)
	if !ok {
		return errors.ErrorWithStackf(
			"receipt %s: unexpected blob shape %T (expected *V1)",
			&receiptId, blob)
	}

	if err := validateEntries(v1.Entries, dest); err != nil {
		return err
	}

	return materializeEntries(blobStore, v1.Entries, dest)
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

// validateEntries runs the RFC 0003 §Consumer Rules §Path Sanitization
// checks across every entry. Returns the first failure as a refusal
// error — atomic per the FDR: any refusal aborts phase A before any
// disk write.
//
// The materialized path used for the boundary check is
// `filepath.Clean(filepath.Join(dest, e.root, e.path))`. The
// destination boundary is `filepath.Clean(dest)`.
//
// The `error: ` prefix in the FDR-quoted diagnostics is added by the
// framework via ContextCancelWithBadRequestError; the strings here
// start at the noun.
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

// materializeEntries iterates entries in receipt order and writes each
// to disk per its type. Per FDR §Per-Type Materialization:
//
//	file    → create-only open + io.Copy + chmod
//	dir     → MkdirAll
//	symlink → os.Symlink with literal target; mode ignored
//	other   → skip with stderr notice
//
// The destination root is created (MkdirAll, mode 0o755) before
// iteration so file/symlink writes inside top-level subdirs of <dest>
// don't fail when their parent dir entry hasn't been visited yet.
//
// Per FDR §Limitations §No mid-stream rollback: a blob-read or write
// failure mid-stream leaves the destination partial; cleanup is the
// operator's job. The diagnostic names the entry that failed.
func materializeEntries(
	blobStore blob_stores.BlobStoreInitialized,
	entries []tree_capture_receipt.EntryV1,
	dest string,
) error {
	cleanDest := filepath.Clean(dest)

	if err := os.MkdirAll(cleanDest, 0o755); err != nil {
		return errors.Wrapf(err, "create destination %q", cleanDest)
	}

	for i := range entries {
		e := entries[i]
		materialized := filepath.Clean(filepath.Join(cleanDest, e.Root, e.Path))

		switch e.Type {
		case tree_capture_receipt.TypeFile:
			if err := materializeFile(blobStore, e, materialized); err != nil {
				return err
			}

		case tree_capture_receipt.TypeDir:
			if err := os.MkdirAll(materialized, e.Mode.Perm()); err != nil {
				return errors.Wrapf(err, "mkdir %q", materialized)
			}

		case tree_capture_receipt.TypeSymlink:
			if err := os.Symlink(e.Target, materialized); err != nil {
				return errors.Wrapf(err, "symlink %q -> %q", materialized, e.Target)
			}

		case tree_capture_receipt.TypeOther:
			fmt.Fprintf(os.Stderr,
				"notice: skipping entry of type %q: %s\n",
				e.Type, materialized)

		default:
			return errors.ErrorWithStackf(
				"%s: unknown entry type %q", materialized, e.Type)
		}
	}

	return nil
}

// materializeFile opens the materialized path create-only, streams the
// blob into it via io.Copy, then applies the captured POSIX
// permissions. domain_interfaces.BlobReader satisfies io.WriterTo, so
// io.Copy uses the WriteTo fast path and the file content is never
// buffered in memory.
func materializeFile(
	blobStore blob_stores.BlobStoreInitialized,
	e tree_capture_receipt.EntryV1,
	materialized string,
) (err error) {
	var blobId markl.Id
	if err = blobId.Set(e.BlobId); err != nil {
		return errors.Wrapf(err, "%s: parse blob_id %q", materialized, e.BlobId)
	}

	reader, err := blobStore.MakeBlobReader(&blobId)
	if err != nil {
		return errors.Wrapf(err, "%s: open blob %s", materialized, &blobId)
	}
	defer errors.DeferredCloser(&err, reader)

	file, err := os.OpenFile(
		materialized,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o666,
	)
	if err != nil {
		return errors.Wrapf(err, "create %q", materialized)
	}
	defer errors.DeferredCloser(&err, file)

	if _, err = io.Copy(file, reader); err != nil {
		return errors.Wrapf(err,
			"%s: blob read failed\n  blob_id: %s",
			materialized, &blobId)
	}

	if err = os.Chmod(materialized, e.Mode.Perm()); err != nil {
		return errors.Wrapf(err, "chmod %q", materialized)
	}

	return nil
}
