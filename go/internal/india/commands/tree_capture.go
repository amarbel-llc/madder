package commands

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	utility.AddCmd("tree-capture", &TreeCapture{
		Format: output_format.Default,
	})
}

type TreeCapture struct {
	command_components.EnvBlobStore

	Format output_format.Format
}

var (
	_ interfaces.CommandComponentWriter = (*TreeCapture)(nil)
	_ futility.CommandWithParams        = (*TreeCapture)(nil)
)

func (cmd *TreeCapture) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "args",
			Description: "directories to capture, optionally interleaved with blob-store-ids that switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd TreeCapture) GetDescription() futility.Description {
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
			"string; the target is not dereferenced, and a symlink " +
			"passed as a capture-root is rejected (resolve it with " +
			"realpath first). Hidden files are captured. With zero " +
			"positional arguments the current working directory is " +
			"captured into the default store; with exactly one " +
			"positional argument that resolves to a blob-store-id " +
			"the current working directory is captured into that " +
			"store. Output format defaults to TAP on an interactive " +
			"terminal and to NDJSON when stdout is piped; pass " +
			"-format to force a specific encoding.",
	}
}

func (cmd TreeCapture) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *TreeCapture) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

func (cmd TreeCapture) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	args := req.PopArgs()
	shadowCandidates := command_components.BlobStoreIds(envBlobStore.GetBlobStores())

	groups, classifyFails, planErr := planCapture(args, shadowCandidates)

	var sink tree_capture_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		sink = tree_capture_sink.NewNDJSON(os.Stdout, os.Stderr)
	default:
		sink = tree_capture_sink.NewTAP(os.Stdout)
	}

	failCount := 0

	for _, cf := range classifyFails {
		sink.Failure(cf.arg, cf.err)
		failCount++
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
		if group.storeID.IsEmpty() {
			blobStore = envBlobStore.GetDefaultBlobStore()
		} else {
			blobStore = envBlobStore.GetBlobStore(group.storeID)
			storeName = group.storeID.String()
		}

		sink.SetStore(storeName)

		var entries []tree_capture_receipt.Entry

		for _, root := range group.roots {
			if root.shadowNotice != "" {
				sink.Notice(root.shadowNotice)
			}
			fails := walkRoot(blobStore, root.path, &entries, sink)
			failCount += fails
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
			failCount++
			continue
		}

		sink.StoreGroupReceipt(receiptID, len(entries))
	}

	sink.Finalize()

	if failCount > 0 {
		errors.ContextCancelWithBadRequestf(
			req,
			"tree-capture failed entries: %d",
			failCount,
		)
		return
	}
}

type captureRoot struct {
	path         string
	shadowNotice string
}

// captureGroup is the unit of one receipt: a target store plus the set
// of directories captured into it. An empty storeID selects the default
// store. switchNotice is non-empty when this group started with an
// explicit store-switch arg; the planner emits it before the group's
// first root is walked.
type captureGroup struct {
	storeID      blob_store_id.Id
	switchNotice string
	roots        []captureRoot
}

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
	if len(args) == 0 {
		return []captureGroup{{
			roots: []captureRoot{{path: "."}},
		}}, nil, nil
	}

	if len(args) == 1 {
		k := classifyArg(args[0])
		switch k.kind {
		case argKindStoreId:
			return []captureGroup{{
				storeID:      k.storeID,
				switchNotice: arg_resolver.FormatStoreSwitchNotice(k.storeID),
				roots:        []captureRoot{{path: "."}},
			}}, nil, nil
		case argKindDir:
			if scopeErr := checkRootScope(args[0]); scopeErr != nil {
				return nil, []classifyFailure{{arg: args[0], err: scopeErr}},
					errors.ErrorWithStackf("no usable directories or store-ids in arguments")
			}
			return []captureGroup{{
				roots: []captureRoot{{
					path:         args[0],
					shadowNotice: shadowNoticeFor(args[0], shadowCandidates),
				}},
			}}, nil, nil
		case argKindError:
			return nil, []classifyFailure{{arg: args[0], err: k.err}},
				errors.ErrorWithStackf("no usable directories or store-ids in arguments")
		}
	}

	current := captureGroup{}
	flush := func() error {
		if collisionErr := checkRootCollisions(current.roots); collisionErr != nil {
			return collisionErr
		}
		groups = append(groups, current)
		return nil
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
			if len(current.roots) == 0 && !current.storeID.IsEmpty() {
				err = errors.ErrorWithStackf(
					"blob-store-id %q has no following directories",
					current.storeID,
				)
				return
			}
			if len(current.roots) > 0 {
				if err = flush(); err != nil {
					return
				}
			}

			current = captureGroup{
				storeID:      k.storeID,
				switchNotice: arg_resolver.FormatStoreSwitchNotice(k.storeID),
			}

		case argKindDir:
			if scopeErr := checkRootScope(arg); scopeErr != nil {
				classifyFails = append(classifyFails, classifyFailure{
					arg: arg,
					err: scopeErr,
				})
				continue
			}
			current.roots = append(current.roots, captureRoot{
				path:         arg,
				shadowNotice: shadowNoticeFor(arg, shadowCandidates),
			})
		}
	}

	if len(current.roots) > 0 {
		if err = flush(); err != nil {
			return
		}
	} else if !current.storeID.IsEmpty() {
		err = errors.ErrorWithStackf(
			"blob-store-id %q has no following directories",
			current.storeID,
		)
		return
	}

	if len(groups) == 0 && len(classifyFails) == 0 {
		err = errors.ErrorWithStackf(
			"no usable directories or store-ids in arguments",
		)
		return
	}

	return
}

func shadowNoticeFor(arg string, candidates []blob_store_id.Id) string {
	shadowed, ok := arg_resolver.DetectShadow(arg, candidates)
	if !ok {
		return ""
	}
	return arg_resolver.FormatShadowWarning(arg, shadowed)
}

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
// blob-store-id. Uses Lstat (not Stat) so a symlink-to-directory does
// not classify as a directory: filepath.WalkDir would refuse to descend
// it anyway, and the resulting one-entry "type=symlink" receipt would
// surprise a user who expected the linked tree's contents. Users who
// want that should resolve the symlink before passing it in.
func classifyArg(arg string) classifiedArg {
	info, err := os.Lstat(arg)
	switch {
	case err == nil && info.IsDir():
		return classifiedArg{kind: argKindDir}
	case err == nil:
		return classifiedArg{
			kind: argKindError,
			err: errors.ErrorWithStackf(
				"%q exists but is not a directory; tree-capture only takes directories (resolve symlinks with realpath if needed)",
				arg,
			),
		}
	case errors.IsNotExist(err):
		// fall through to store-id parsing
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

// checkRootScope refuses dir args that resolve outside PWD per RFC
// 0003 §Producer Rules §Root Scoping. Mirrors git's "outside
// repository" diagnostic; PWD is the implicit work tree.
func checkRootScope(arg string) error {
	pwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err)
	}

	abs, err := filepath.Abs(arg)
	if err != nil {
		return errors.Wrap(err)
	}

	rel, err := filepath.Rel(pwd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.ErrorWithStackf(
			"%s: outside working directory at %s\nhint: cd to a parent directory containing %s, then re-run",
			arg, pwd, arg,
		)
	}

	return nil
}

// checkRootCollisions refuses two roots within a single store-group
// that resolve to the same path under filepath.Clean per RFC 0003
// §Producer Rules §Root Collision Detection.
func checkRootCollisions(roots []captureRoot) error {
	seen := make(map[string]string, len(roots))

	for _, r := range roots {
		clean := filepath.Clean(r.path)
		if first, ok := seen[clean]; ok {
			return errors.ErrorWithStackf(
				"roots %q and %q both resolve to %q after Clean\nhint: pass each directory only once per store-group",
				first, r.path, clean,
			)
		}
		seen[clean] = r.path
	}

	return nil
}

// walkRoot walks rootArg with filepath.WalkDir (which does not follow
// symlinks), writes every regular file as a blob into store, appends a
// tree_capture_receipt.Entry for every entry it visited, and emits live
// per-entry events on the sink. Returns the count of per-entry
// failures encountered during the walk.
func walkRoot(
	store blob_stores.BlobStoreInitialized,
	rootArg string,
	accum *[]tree_capture_receipt.Entry,
	sink tree_capture_sink.Sink,
) int {
	var failCount int

	walkErr := filepath.WalkDir(rootArg, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			sink.Failure(p, walkErr)
			failCount++
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

		mode := info.Mode()
		entry := tree_capture_receipt.Entry{
			Path: rel,
			Root: rootArg,
			Mode: mode,
		}

		switch {
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				sink.Failure(p, errors.Wrap(err))
				failCount++
				return nil
			}
			entry.Type = tree_capture_receipt.TypeSymlink
			entry.Target = target

		case mode.IsDir():
			entry.Type = tree_capture_receipt.TypeDir

		case mode.IsRegular():
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
		sink.Entry(entry)
		return nil
	})

	if walkErr != nil {
		sink.Failure(rootArg, errors.Wrap(walkErr))
		failCount++
	}

	return failCount
}

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
// into a new blob in blobStore. The output is deterministic:
// equivalent inputs yield byte-identical receipts and identical blob
// IDs.
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
