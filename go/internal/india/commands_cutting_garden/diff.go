package commands_cutting_garden

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/capture_receipt"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("diff", &Diff{
		EnvBlobStore: command_components.EnvBlobStore{BlobStoreXDGScope: "madder"},
	})
}

// Diff implements `cutting-garden diff <receipt-id> <dir>`.
//
// The on-disk tree at <dir> is walked through a discard blob store
// (foxtrot/blob_stores.NewDiscardBlobStore) so file content blob-ids
// are recomputed in the receipt's source-store hash family without
// persisting anything. Each disk entry is then compared with the
// receipt entry that materializes to the same rel-to-<dir> path; any
// missing/extra entry, type change, mode change, blob change, or
// symlink-target change becomes a single line on stdout.
//
// Output is plain text, one diff per line, sorted by path. Receipt
// store-hint resolution and the -store override mirror restore.go
// exactly.
//
// Exit code: 0 when the tree matches the receipt; non-zero when any
// difference is found OR an error occurs. The two are distinguishable
// from stderr (a clean diff prints `diff: N differences`; an error
// prints the framework's error tree).
type Diff struct {
	command_components.EnvBlobStore

	// Store mirrors Restore.Store: when non-empty, overrides the
	// receipt's store-hint resolution per FDR 0001 §Store-Hint
	// Resolution branch 1.
	Store string
}

var (
	_ interfaces.CommandComponentWriter = (*Diff)(nil)
	_ futility.CommandWithParams        = (*Diff)(nil)
)

func (cmd *Diff) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "receipt-id",
			Description: "markl-id of a cutting_garden-capture_receipt-fs-v1 blob",
		},
		futility.Arg[*values.String]{
			Name:        "dir",
			Description: "directory to compare against the receipt; MUST exist",
		},
	}
}

func (cmd *Diff) GetDescription() futility.Description {
	return futility.Description{
		Short: "compare a directory tree against a capture receipt",
		Long: "Walk <dir> and compare its contents against the entries " +
			"in <receipt-id>. Each entry is matched by its rel-to-<dir> " +
			"materialization path. For files, content blob-ids are " +
			"recomputed in the receipt's source-store hash family via " +
			"a discard blob store (no bytes are persisted). Output is " +
			"one line per difference: 'M' for modified mode/blob/" +
			"target, 'A' for entries on disk but not in the receipt, " +
			"'D' for entries in the receipt but not on disk, 'T' for " +
			"a changed type. Exit 0 when the tree matches the receipt; " +
			"non-zero on any difference or error. The receipt's source " +
			"store is resolved via the same hint-driven logic as " +
			"`restore`; pass -store to override.",
	}
}

func (cmd *Diff) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
}

func (cmd *Diff) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&cmd.Store,
		"store",
		"",
		"explicit blob-store-id to resolve the receipt against "+
			"(overrides the receipt's store-hint resolution)",
	)
}

func (cmd *Diff) Run(req futility.Request) {
	receiptIdStr := req.PopArg("receipt-id")
	dir := req.PopArg("dir")
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	if err := cmd.runDiff(envBlobStore, receiptIdStr, dir); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
}

// runDiff is the testable core: resolves the receipt, walks <dir>,
// reports every difference. Returns a non-nil error when there is
// either a real failure (receipt not found, dir missing, walk error)
// or any difference between tree and receipt — both surface as
// errors.ContextCancelWithBadRequestError in Run, mapping to exit 1.
func (cmd *Diff) runDiff(
	envBlobStore command_components.BlobStoreEnv,
	receiptIdStr string,
	dir string,
) error {
	if err := assertDirectoryExists(dir); err != nil {
		return err
	}

	var receiptId markl.Id
	if err := receiptId.Set(receiptIdStr); err != nil {
		return errors.ErrorWithStackf(
			"parse receipt-id %q: %v", receiptIdStr, err,
		)
	}

	blob, err := readReceiptBlob(envBlobStore, &receiptId, cmd.Store)
	if err != nil {
		return err
	}

	v1, ok := blob.(*capture_receipt.V1)
	if !ok {
		return errors.ErrorWithStackf(
			"receipt %s: unexpected blob shape %T (expected *V1)",
			&receiptId, blob)
	}

	if err := validateEntries(v1.Entries, dir); err != nil {
		return err
	}

	sourceStore, err := resolveMaterializationStore(
		envBlobStore, v1.Hint, cmd.Store,
	)
	if err != nil {
		return err
	}

	discardStore := blob_stores.NewDiscardBlobStore(
		sourceStore.GetDefaultHashType(),
	)

	diskEntries, walkErr := walkForDiff(discardStore, dir)
	if walkErr != nil {
		return walkErr
	}

	differences := compareEntries(v1.Entries, diskEntries)

	for _, line := range differences {
		fmt.Fprintln(os.Stdout, line)
	}

	if len(differences) > 0 {
		fmt.Fprintf(os.Stderr, "diff: %d differences\n", len(differences))
		return errors.ErrorWithStackf(
			"tree differs from receipt: %d %s",
			len(differences), pluralize("entry", "entries", len(differences)),
		)
	}

	return nil
}

func assertDirectoryExists(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrorWithStackf(
				"%s: directory does not exist\n"+
					"hint: pass an existing directory; diff does not " +
					"materialize anything",
				dir,
			)
		}
		return errors.Wrapf(err, "stat %q", dir)
	}
	if !info.IsDir() {
		return errors.ErrorWithStackf(
			"%s: not a directory (mode %s)", dir, info.Mode(),
		)
	}
	return nil
}

// walkForDiff walks dir with filepath.WalkDir (lstat-based, no symlink
// follow — same rule as capture.walkRoot at capture.go:485) and emits
// one EntryV1 per visited path. Regular files are streamed through
// store.MakeBlobWriter so their blob-ids are computed under the
// store's hash family. Returns an aggregate error when the walk itself
// fails or when one or more per-entry failures occur. Per-entry
// failures are appended to the error message; this matches diff's
// "first reportable problem aborts" semantics — diff is read-only, so
// a partial walk has no recovery story.
func walkForDiff(
	store blob_stores.BlobStoreInitialized,
	dir string,
) (entries []capture_receipt.EntryV1, err error) {
	var perEntryFailures []string

	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			perEntryFailures = append(perEntryFailures,
				fmt.Sprintf("%s: %v", p, walkErr))
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			perEntryFailures = append(perEntryFailures,
				fmt.Sprintf("%s: %v", p, err))
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			perEntryFailures = append(perEntryFailures,
				fmt.Sprintf("%s: %v", p, err))
			return nil
		}
		rel = filepath.ToSlash(rel)

		mode := info.Mode()
		entry := capture_receipt.EntryV1{
			Path: rel,
			Root: dir,
			Mode: mode,
		}

		switch {
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				perEntryFailures = append(perEntryFailures,
					fmt.Sprintf("%s: %v", p, err))
				return nil
			}
			entry.Type = capture_receipt.TypeSymlink
			entry.Target = target

		case mode.IsDir():
			entry.Type = capture_receipt.TypeDir

		case mode.IsRegular():
			id, size, err := hashFileViaStore(store, p)
			if err != nil {
				perEntryFailures = append(perEntryFailures,
					fmt.Sprintf("%s: %v", p, err))
				return nil
			}
			entry.Type = capture_receipt.TypeFile
			entry.Size = size
			entry.BlobId = id.String()

		default:
			entry.Type = capture_receipt.TypeOther
		}

		entries = append(entries, entry)
		return nil
	})

	if walkErr != nil {
		return nil, errors.Wrapf(walkErr, "walk %q", dir)
	}
	if len(perEntryFailures) > 0 {
		return nil, errors.ErrorWithStackf(
			"%d entry walk failures:\n  %s",
			len(perEntryFailures),
			joinLines(perEntryFailures),
		)
	}

	return entries, nil
}

// hashFileViaStore is the discard-store analogue of capture.go's
// writeFileBlob — same shape, same MakeBlobWriter call, but the
// underlying store sends bytes to io.Discard. Only the digester half
// of the chain matters for diff.
func hashFileViaStore(
	store blob_stores.BlobStoreInitialized,
	srcPath string,
) (id domain_interfaces.MarklId, size int64, err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		err = errors.Wrap(err)
		return
	}
	defer errors.DeferredCloser(&err, src)

	wc, err := store.MakeBlobWriter(nil)
	if err != nil {
		err = errors.Wrap(err)
		return
	}
	defer errors.DeferredCloser(&err, wc)

	if size, err = io.Copy(wc, src); err != nil {
		err = errors.Wrap(err)
		return
	}

	id = wc.GetMarklId()
	return
}

// compareEntries computes the per-path symmetric difference between
// receipt entries and on-disk entries. The key for both sides is the
// rel-to-<dir> materialization path (filepath.Clean(filepath.Join(
// e.Root, e.Path)) for receipt entries; filepath.Clean(e.Path) for
// disk entries since walkForDiff already records Path as rel-to-<dir>).
// Output lines are sorted by path.
func compareEntries(
	receipt []capture_receipt.EntryV1,
	disk []capture_receipt.EntryV1,
) []string {
	receiptByPath := make(map[string]capture_receipt.EntryV1, len(receipt))
	for _, e := range receipt {
		key := filepath.ToSlash(filepath.Clean(filepath.Join(e.Root, e.Path)))
		receiptByPath[key] = e
	}

	diskByPath := make(map[string]capture_receipt.EntryV1, len(disk))
	for _, e := range disk {
		key := filepath.ToSlash(filepath.Clean(e.Path))
		diskByPath[key] = e
	}

	// The on-disk "." entry is the dir argument itself — the container
	// in which the receipt's tree materializes, not part of the receipt
	// unless the receipt was captured with Root="." and Path=".". When
	// the receipt has no "." key, the dir's own mode is conceptually
	// outside the comparison and reporting it as `A  .` is noise.
	if _, ok := receiptByPath["."]; !ok {
		delete(diskByPath, ".")
	}

	allKeys := make(map[string]struct{}, len(receiptByPath)+len(diskByPath))
	for k := range receiptByPath {
		allKeys[k] = struct{}{}
	}
	for k := range diskByPath {
		allKeys[k] = struct{}{}
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		recv, inReceipt := receiptByPath[k]
		dsk, onDisk := diskByPath[k]

		switch {
		case inReceipt && !onDisk:
			lines = append(lines, fmt.Sprintf("D  %s\t%s", k, recv.Type))
		case onDisk && !inReceipt:
			lines = append(lines, fmt.Sprintf("A  %s\t%s", k, dsk.Type))
		case recv.Type != dsk.Type:
			lines = append(lines, fmt.Sprintf("T  %s\t%s -> %s",
				k, recv.Type, dsk.Type))
		default:
			lines = append(lines, perTypeDiffs(k, recv, dsk)...)
		}
	}

	return lines
}

// perTypeDiffs emits zero or more lines comparing the type-specific
// fields of two entries known to share the same type. Diff is reported
// per-attribute so a single path can produce multiple lines (e.g. mode
// AND blob differ → two lines).
func perTypeDiffs(
	path string,
	recv, dsk capture_receipt.EntryV1,
) []string {
	var out []string

	switch recv.Type {
	case capture_receipt.TypeFile:
		if recv.Mode.Perm() != dsk.Mode.Perm() {
			out = append(out, fmt.Sprintf("M  %s\tmode %s -> %s",
				path, recv.Mode.Perm(), dsk.Mode.Perm()))
		}
		if recv.BlobId != dsk.BlobId {
			out = append(out, fmt.Sprintf("M  %s\tblob %s -> %s",
				path, recv.BlobId, dsk.BlobId))
		}

	case capture_receipt.TypeDir:
		if recv.Mode.Perm() != dsk.Mode.Perm() {
			out = append(out, fmt.Sprintf("M  %s\tmode %s -> %s",
				path, recv.Mode.Perm(), dsk.Mode.Perm()))
		}

	case capture_receipt.TypeSymlink:
		if recv.Target != dsk.Target {
			out = append(out, fmt.Sprintf("M  %s\ttarget %q -> %q",
				path, recv.Target, dsk.Target))
		}

	case capture_receipt.TypeOther:
		// Capture is lossy here (FDR 0001 §Limitations): the receipt
		// records "other" with no comparable content, and the disk walk
		// likewise can't classify it. Skip — emitting nothing is the
		// honest answer.
	}

	return out
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}

func pluralize(singular, plural string, n int) string {
	if n == 1 {
		return singular
	}
	return plural
}
