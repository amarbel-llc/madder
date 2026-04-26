package commands

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/charlie/tree_capture_receipt"
	"github.com/amarbel-llc/madder/go/internal/charlie/tree_capture_sink"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("capture-tree", &CaptureTree{
		Format: output_format.Default,
	})
}

type CaptureTree struct {
	command_components.EnvBlobStore

	Format output_format.Format
}

var (
	_ interfaces.CommandComponentWriter = (*CaptureTree)(nil)
	_ futility.CommandWithParams        = (*CaptureTree)(nil)
)

func (cmd *CaptureTree) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "args",
			Description: "directories to capture, optionally interleaved with blob-store-ids that switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd CaptureTree) GetDescription() futility.Description {
	return futility.Description{
		Short: "capture a directory tree into a blob store",
		Long: "Walk one or more directories and write every regular " +
			"file as a content-addressable blob into the active " +
			"store. Each capture run produces one receipt blob per " +
			"store-group: a hyphence-wrapped NDJSON document " +
			"(`! madder-tree_capture-receipt-v1`) listing every " +
			"captured entry with its relative path, type, POSIX " +
			"permission bits, blob ID (or symlink target), and " +
			"size. The receipt blob ID is reported on stdout. " +
			"Arguments are positional; a bare arg that resolves to " +
			"a directory is captured under the active store, while " +
			"a blob-store-id switches the active store for subsequent " +
			"directories. Blob-store-ids support optional prefixes " +
			"that select the XDG scope: '.' for CWD-relative, '/' " +
			"for system-wide, '%' for cache, '_' for custom-rooted, " +
			"and no prefix for the user default — see " +
			"blob-store(7). Symlinks are recorded by their target " +
			"string; the target is not dereferenced. Hidden files " +
			"are captured. With zero positional arguments the " +
			"current working directory is captured into the default " +
			"store; with exactly one positional argument that " +
			"resolves to a blob-store-id the current working " +
			"directory is captured into that store. Output format " +
			"defaults to TAP on an interactive terminal and to " +
			"NDJSON when stdout is piped; pass -format to force a " +
			"specific encoding.",
	}
}

func (cmd CaptureTree) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *CaptureTree) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

func (cmd CaptureTree) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	args := req.PopArgs()
	allStores := envBlobStore.GetBlobStores()
	shadowCandidates := command_components.BlobStoreIds(allStores)

	groups, classifyFails, planErr := planCapture(args, shadowCandidates)

	var sink tree_capture_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		sink = tree_capture_sink.NewNDJSON(os.Stdout, os.Stderr)
	default:
		sink = tree_capture_sink.NewTAP(os.Stdout)
	}

	var failCount atomic.Uint32

	for _, cf := range classifyFails {
		sink.Failure(cf.arg, cf.err)
		failCount.Add(1)
	}

	if planErr != nil {
		sink.Failure("(arguments)", planErr)
		sink.Finalize()
		errors.ContextCancelWithBadRequestf(req, "%s", planErr.Error())
		return
	}

	for _, group := range groups {
		if group.switchNotice != "" {
			sink.Notice(group.switchNotice)
		}

		var blobStore blob_stores.BlobStoreInitialized
		var storeName string
		if group.useDefault {
			blobStore = envBlobStore.GetDefaultBlobStore()
		} else {
			blobStore = envBlobStore.GetBlobStore(group.storeID)
			storeName = group.storeID.String()
		}

		var entries []tree_capture_receipt.Entry

		for _, root := range group.roots {
			if root.shadowNotice != "" {
				sink.Notice(root.shadowNotice)
			}
			fails := walkRoot(blobStore, storeName, root.path, &entries, sink)
			failCount.Add(uint32(fails))
		}

		if len(entries) == 0 {
			sink.Notice(fmt.Sprintf(
				"notice: no entries captured for store=%s; receipt skipped",
				quoteEmpty(storeName),
			))
			continue
		}

		receiptID, err := writeReceiptBlob(blobStore, entries)
		if err != nil {
			sink.Failure(
				fmt.Sprintf("(receipt:%s)", quoteEmpty(storeName)),
				err,
			)
			failCount.Add(1)
			continue
		}

		sink.StoreGroupReceipt(storeName, receiptID, len(entries))
	}

	sink.Finalize()

	if fc := failCount.Load(); fc > 0 {
		errors.ContextCancelWithBadRequestf(
			req,
			"capture-tree failed entries: %d",
			fc,
		)
		return
	}
}

// captureRoot is one directory the walk should descend, optionally
// carrying a shadow-warning notice the caller should emit before walking.
type captureRoot struct {
	path         string
	shadowNotice string
}

// captureGroup is the unit of one receipt: a target store plus the set
// of directories captured into it. switchNotice is non-empty when this
// group started with an explicit store-switch arg; the planner emits it
// at the first dir of the group.
type captureGroup struct {
	useDefault   bool
	storeID      blob_store_id.Id
	switchNotice string
	roots        []captureRoot
}

// classifyFailure carries one per-arg classification error for the
// caller to surface on the sink.
type classifyFailure struct {
	arg string
	err error
}

// planCapture splits args into store groups and validates that each
// group has at least one capture-root. Returns either valid groups (and
// the per-arg classification failures, if any) or a planning error
// describing an empty store group. PWD is the implicit root in two
// cases: zero args, and a single arg that classifies as a store-id.
func planCapture(
	args []string,
	shadowCandidates []blob_store_id.Id,
) (groups []captureGroup, classifyFails []classifyFailure, err error) {
	// Zero-arg fast path.
	if len(args) == 0 {
		return []captureGroup{{
			useDefault: true,
			roots:      []captureRoot{{path: "."}},
		}}, nil, nil
	}

	// Single-arg fast path: if the lone arg classifies as a store-id
	// (and not a directory in CWD), default the root to PWD.
	if len(args) == 1 {
		k := classifyArg(args[0])
		switch k.kind {
		case argKindStoreId:
			return []captureGroup{{
				storeID: k.storeID,
				switchNotice: arg_resolver.FormatStoreSwitchNotice(
					k.storeID,
				),
				roots: []captureRoot{{path: "."}},
			}}, nil, nil
		case argKindDir:
			notice := ""
			if shadowed, ok := arg_resolver.DetectShadow(
				args[0], shadowCandidates,
			); ok {
				notice = arg_resolver.FormatShadowWarning(args[0], shadowed)
			}
			return []captureGroup{{
				useDefault: true,
				roots: []captureRoot{{
					path:         args[0],
					shadowNotice: notice,
				}},
			}}, nil, nil
		case argKindError:
			return nil, []classifyFailure{{arg: args[0], err: k.err}}, errPlanNoUsableArgs
		}
	}

	// Multi-arg path: classify each, build groups, validate.
	current := captureGroup{useDefault: true}
	flush := func() {
		groups = append(groups, current)
	}

	for _, arg := range args {
		k := classifyArg(arg)

		switch k.kind {
		case argKindError:
			classifyFails = append(classifyFails, classifyFailure{
				arg: arg,
				err: k.err,
			})

		case argKindStoreId:
			if len(current.roots) == 0 && (current.useDefault || !current.storeID.IsEmpty()) {
				// The current group never received a dir.
				if !current.useDefault {
					// Previous explicit store-switch had no dirs.
					err = errors.ErrorWithStackf(
						"blob-store-id %q has no following directories",
						current.storeID,
					)
					return
				}
				// useDefault group with no roots and no explicit
				// switch yet — drop it silently and start fresh.
			} else {
				flush()
			}

			current = captureGroup{
				storeID: k.storeID,
				switchNotice: arg_resolver.FormatStoreSwitchNotice(
					k.storeID,
				),
			}

		case argKindDir:
			notice := ""
			if shadowed, ok := arg_resolver.DetectShadow(
				arg, shadowCandidates,
			); ok {
				notice = arg_resolver.FormatShadowWarning(arg, shadowed)
			}
			current.roots = append(current.roots, captureRoot{
				path:         arg,
				shadowNotice: notice,
			})
		}
	}

	// Flush the trailing group (must have ≥1 root unless the only
	// thing that ever happened was classifyError args).
	if len(current.roots) > 0 {
		flush()
	} else if !current.useDefault {
		err = errors.ErrorWithStackf(
			"blob-store-id %q has no following directories",
			current.storeID,
		)
		return
	}

	if len(groups) == 0 && len(classifyFails) == 0 {
		// Defensive: shouldn't happen given the branches above, but
		// guards against silent no-op.
		err = errPlanNoUsableArgs
		return
	}

	return
}

var errPlanNoUsableArgs = errors.ErrorWithStackf("no usable directories or store-ids in arguments")

type argKind int

const (
	argKindError argKind = iota
	argKindDir
	argKindStoreId
)

type classifiedArg struct {
	kind    argKind
	storeID blob_store_id.Id
	err     error
}

// classifyArg decides whether arg names a directory in CWD or a
// blob-store-id. Mirrors arg_resolver's file-first precedence: a bare
// arg that exists as a directory is treated as a directory (with shadow
// detection handled by the caller), and only falls through to
// blob-store-id classification when no such directory exists.
func classifyArg(arg string) classifiedArg {
	info, err := os.Stat(arg)
	switch {
	case err == nil && info.IsDir():
		return classifiedArg{kind: argKindDir}
	case err == nil:
		return classifiedArg{
			kind: argKindError,
			err: errors.ErrorWithStackf(
				"%q exists but is not a directory; capture-tree only takes directories",
				arg,
			),
		}
	case errors.IsNotExist(err):
		// Fall through to store-id parsing.
	default:
		return classifiedArg{kind: argKindError, err: errors.Wrap(err)}
	}

	var id blob_store_id.Id
	if perr := id.Set(arg); perr == nil {
		return classifiedArg{kind: argKindStoreId, storeID: id}
	}

	return classifiedArg{
		kind: argKindError,
		err: errors.ErrorWithStackf(
			"%q is neither an existing directory nor a valid blob-store-id",
			arg,
		),
	}
}

// walkRoot walks rootArg with filepath.WalkDir (which does not follow
// symlinks), writes every regular file as a blob into store, appends a
// tree_capture_receipt.Entry for every entry it visited, and emits live
// per-entry events on the sink. Returns the count of per-entry
// failures encountered during the walk.
func walkRoot(
	store blob_stores.BlobStoreInitialized,
	storeName string,
	rootArg string,
	accum *[]tree_capture_receipt.Entry,
	sink tree_capture_sink.Sink,
) int {
	var failCount int

	walkErr := filepath.WalkDir(rootArg, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			sink.Failure(p, walkErr)
			failCount++
			// Don't propagate; let WalkDir continue with siblings.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			sink.Failure(p, errors.Wrap(err))
			failCount++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(rootArg, p)
		if err != nil {
			sink.Failure(p, errors.Wrap(err))
			failCount++
			return nil
		}
		rel = filepath.ToSlash(rel)

		entry := tree_capture_receipt.Entry{
			Path: rel,
			Root: rootArg,
			Mode: info.Mode(),
		}

		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				sink.Failure(p, errors.Wrap(err))
				failCount++
				return nil
			}
			entry.Type = tree_capture_receipt.TypeSymlink
			entry.Target = target

		case info.Mode().IsDir():
			entry.Type = tree_capture_receipt.TypeDir

		case info.Mode().IsRegular():
			id, size, err := writeFileBlob(store, p)
			if err != nil {
				sink.Failure(p, errors.Wrap(err))
				failCount++
				return nil
			}
			entry.Type = tree_capture_receipt.TypeFile
			entry.Size = size
			entry.BlobId = id.String()

		default:
			entry.Type = tree_capture_receipt.TypeOther
		}

		*accum = append(*accum, entry)
		sink.Entry(storeName, entry)
		return nil
	})

	if walkErr != nil {
		sink.Failure(rootArg, errors.Wrap(walkErr))
		failCount++
	}

	return failCount
}

// writeFileBlob streams srcPath into a new blob in blobStore. Returns
// the blob's MarklId and the number of bytes copied.
func writeFileBlob(
	blobStore blob_stores.BlobStoreInitialized,
	srcPath string,
) (id domain_interfaces.MarklId, size int64, err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		err = errors.Wrap(err)
		return
	}
	defer errors.DeferredCloser(&err, src)

	wc, err := blobStore.MakeBlobWriter(nil)
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

// writeReceiptBlob serializes entries via tree_capture_receipt.Write
// into a new blob in blobStore. Returns the receipt blob's stringified
// MarklId. The output is deterministic — equivalent inputs yield
// byte-identical receipts and identical blob IDs.
func writeReceiptBlob(
	blobStore blob_stores.BlobStoreInitialized,
	entries []tree_capture_receipt.Entry,
) (id string, err error) {
	wc, err := blobStore.MakeBlobWriter(nil)
	if err != nil {
		err = errors.Wrap(err)
		return
	}
	defer errors.DeferredCloser(&err, wc)

	if _, err = tree_capture_receipt.Write(wc, entries); err != nil {
		err = errors.Wrap(err)
		return
	}

	id = wc.GetMarklId().String()
	return
}

func quoteEmpty(s string) string {
	if s == "" {
		return `(default)`
	}
	return s
}
