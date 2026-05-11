package commands_cutting_garden

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

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
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() {
	utility.AddCmd("diff", &Diff{
		EnvBlobStore: command_components.EnvBlobStore{BlobStoreXDGScope: "madder"},
		Color:        "auto",
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

	// VerifyBlobsExist toggles the receipt-vs-store check on top of
	// the default tree-vs-receipt comparison. When true, every
	// receipt entry with a non-empty BlobId is probed via
	// sourceStore.HasBlob; missing blobs surface as `B` lines and
	// count toward the differences total. Default false: a clean
	// diff exit only proves the tree matches the receipt's records,
	// not that the receipt is fully restorable.
	VerifyBlobsExist bool

	// Color toggles ANSI SGR coloring of the per-line markers
	// (A/D/M/T/B). One of "auto" (default), "always", "never".
	// "auto" enables color when stdout is a TTY and NO_COLOR is
	// unset. Validated in runDiff.
	Color string
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
	flagSet.BoolVar(
		&cmd.VerifyBlobsExist,
		"verify-blobs-exist",
		false,
		"in addition to the tree-vs-receipt comparison, probe the "+
			"resolved source store for every file entry's blob and "+
			"emit a B line when a referenced blob is missing. "+
			"Catches receipts whose referenced blobs have been gc'd "+
			"or that were hand-crafted with bogus ids. Off by default "+
			"because it adds one HasBlob round-trip per file entry, "+
			"which is meaningful on remote (e.g. SFTP) stores.",
	)
	flagSet.StringVar(
		&cmd.Color,
		"color",
		"auto",
		"ANSI SGR coloring of diff markers (A/D/M/T/B): "+
			"auto (default; on when stdout is a TTY and NO_COLOR is "+
			"unset), always, or never",
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
	dirStr string,
) error {
	dirURL, plugin, err := resolveDiffPlugin(dirStr)
	if err != nil {
		return err
	}

	if err := plugin.ValidateDiffDir(dirURL, dirStr); err != nil {
		return err
	}

	var receiptId markl.Id
	if err := receiptId.Set(receiptIdStr); err != nil {
		return errors.ErrorWithStackf(
			"parse receipt-id %q: %v", receiptIdStr, err,
		)
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

	// TODO(amarbel-llc/cutting-garden#18) cross-scheme diffs. Same constraint as restore:
	// today the receipt's type-tag must match the dir plugin's
	// TypeTag(). When cross-scheme restore is allowed, diff should
	// follow the same rule.
	if typeTag.StringSansOp() != plugin.TypeTag() {
		return errors.ErrorWithStackf(
			"receipt %s: type-tag %q cannot be diffed against scheme %q (plugin tag %q); cross-scheme diff is not supported",
			&receiptId, typeTag.StringSansOp(), dirURL.Scheme, plugin.TypeTag(),
		)
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

	diskEntries, walkErr := plugin.ScanForDiff(cutting_garden_plugins.DiffScanRequest{
		Dir:            dirURL,
		RawDir:         dirStr,
		BlobStore:      discardStore,
		ReceiptEntries: v1.Entries,
	})
	if walkErr != nil {
		return walkErr
	}

	var missingBlobs map[string]string
	if cmd.VerifyBlobsExist {
		var probeErr error
		missingBlobs, probeErr = probeMissingBlobs(sourceStore, v1.Entries)
		if probeErr != nil {
			return probeErr
		}
	}

	differences := compareEntries(v1.Entries, diskEntries, missingBlobs)

	renderer, err := newDiffRenderer(cmd.Color, os.Stdout)
	if err != nil {
		return err
	}

	for _, line := range differences {
		fmt.Fprintln(os.Stdout, renderDiffLine(renderer, line))
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

// probeMissingBlobs is the receipt-vs-store check gated by
// -verify-blobs-exist. For every file entry with a non-empty BlobId,
// it parses the id and probes the source store via HasBlob. Returns
// a map keyed by the rel-to-<dir> materialization path (matching the
// key compareEntries uses) → the missing blob-id string. An entry
// whose BlobId fails to parse is treated as missing — that's the
// honest answer when the receipt names something the source store
// cannot address.
func probeMissingBlobs(
	sourceStore blob_stores.BlobStoreInitialized,
	entries []capture_receipt.EntryV1,
) (map[string]string, error) {
	missing := map[string]string{}

	for i := range entries {
		e := entries[i]
		if e.Type != capture_receipt.TypeFile || e.BlobId == "" {
			continue
		}

		key := filepath.ToSlash(filepath.Clean(filepath.Join(e.Root, e.Path)))

		var blobId markl.Id
		if err := blobId.Set(e.BlobId); err != nil {
			missing[key] = e.BlobId
			continue
		}

		if !sourceStore.HasBlob(&blobId) {
			missing[key] = e.BlobId
		}
	}

	return missing, nil
}

// resolveDiffPlugin parses dirStr as a URL and looks up the diff
// plugin registered for its scheme. Schemeless dirs resolve to the
// file plugin's `""` registration.
func resolveDiffPlugin(
	dirStr string,
) (*url.URL, cutting_garden_plugins.DiffPlugin, error) {
	u, err := url.Parse(dirStr)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "parse dir %q", dirStr)
	}
	plugin, err := cutting_garden_plugins.ResolveDiff(u.Scheme)
	if err != nil {
		return nil, nil, err
	}
	return u, plugin, nil
}

// compareEntries computes the per-path symmetric difference between
// receipt entries and on-disk entries. The key for both sides is the
// rel-to-<dir> materialization path (filepath.Clean(filepath.Join(
// e.Root, e.Path)) for receipt entries; filepath.Clean(e.Path) for
// disk entries since walkForDiff already records Path as rel-to-<dir>).
//
// missingBlobs is the receipt-vs-store result from probeMissingBlobs;
// it is keyed identically and may be nil when -verify-blobs-exist
// was not set. Each entry in it produces a `B` line. A path can
// produce both an `M  ... blob` (disk content drifted from receipt)
// AND a `B  ... blob` (receipt's blob is missing in the store);
// they describe orthogonal failures and both are emitted.
//
// Output lines are sorted by line text, which keeps per-path
// groups contiguous (markers sort B < D < M < T alphabetically when
// they share the same path, and "A " sorts on its own since A paths
// have no other lines).
func compareEntries(
	receipt []capture_receipt.EntryV1,
	disk []capture_receipt.EntryV1,
	missingBlobs map[string]string,
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

		if missingId, missing := missingBlobs[k]; missing {
			lines = append(lines, fmt.Sprintf(
				"B  %s\tblob %s missing in source store",
				k, missingId,
			))
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

func pluralize(singular, plural string, n int) string {
	if n == 1 {
		return singular
	}
	return plural
}

// newDiffRenderer builds a lipgloss.Renderer keyed off the -color
// flag value. "auto" (or empty) lets lipgloss/termenv auto-detect
// from stdout (TTY check, NO_COLOR env, COLORTERM, etc.); "always"
// forces ANSI 16-color (enough for the diff marker palette);
// "never" forces Ascii so styles render their input unchanged.
func newDiffRenderer(mode string, stdout *os.File) (*lipgloss.Renderer, error) {
	r := lipgloss.NewRenderer(stdout)
	switch mode {
	case "always":
		r.SetColorProfile(termenv.ANSI)
	case "never":
		r.SetColorProfile(termenv.Ascii)
	case "", "auto":
		// NewRenderer already auto-detected the profile from stdout.
	default:
		return nil, errors.ErrorWithStackf(
			"invalid -color value %q; expected auto, always, or never",
			mode,
		)
	}
	return r, nil
}

// renderDiffLine paints a per-marker color over the entire line.
// Returns the line unchanged when its leading marker isn't one of
// A/D/M/T/B (defensive — keeps the function total).
func renderDiffLine(r *lipgloss.Renderer, line string) string {
	if len(line) == 0 {
		return line
	}
	var color lipgloss.Color
	switch line[0] {
	case 'A':
		color = lipgloss.Color("2") // green   — added on disk
	case 'D':
		color = lipgloss.Color("1") // red     — deleted on disk
	case 'M':
		color = lipgloss.Color("3") // yellow  — modified
	case 'T':
		color = lipgloss.Color("5") // magenta — type-changed
	case 'B':
		color = lipgloss.Color("9") // bright red — receipt blob missing
	default:
		return line
	}
	return r.NewStyle().
		Foreground(color).
		TabWidth(lipgloss.NoTabConversion).
		Render(line)
}
