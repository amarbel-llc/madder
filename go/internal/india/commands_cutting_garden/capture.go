package commands_cutting_garden

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/charlie/capture_receipt"
	"github.com/amarbel-llc/madder/go/internal/charlie/capture_sink"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	_ "github.com/amarbel-llc/madder/go/internal/hotel/cutting_garden_plugin_file"
	"github.com/amarbel-llc/madder/go/internal/hotel/cutting_garden_plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("capture", &Capture{
		EnvBlobStore: command_components.EnvBlobStore{BlobStoreXDGScope: "madder"},
		Format:       output_format.Default,
	})
}

type Capture struct {
	command_components.EnvBlobStore

	Format output_format.Format
}

var (
	_ interfaces.CommandComponentWriter = (*Capture)(nil)
	_ futility.CommandWithParams        = (*Capture)(nil)
)

func (cmd *Capture) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "args",
			Description: "directories to capture, optionally interleaved with blob-store-ids that switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd Capture) GetDescription() futility.Description {
	return futility.Description{
		Short: "capture a directory tree into a blob store",
		Long: "Walk one or more directories and write every regular " +
			"file as a content-addressable blob into the active " +
			"store. Each capture run produces one receipt blob per " +
			"store-group: a hyphence-wrapped NDJSON document " +
			"(`! cutting_garden-capture_receipt-fs-v1`) listing every " +
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

func (cmd Capture) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *Capture) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

func (cmd Capture) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	// cgEnvDir is the cutting-garden-scoped env_dir, distinct from the
	// madder-scoped env_dir embedded in envBlobStore. The two address
	// disjoint XDG paths by construction (proven by
	// env_dir.TestMakeDefault_DistinctScopesAreIndependent). cg uses
	// this for its own audit log; blob writes still go through
	// envBlobStore at madder's scope. See #123 for the multi-scope
	// design.
	cgEnvDir := command_components.MakeEnvDirForScope(req, req.Utility.GetName())

	args := req.PopArgs()
	shadowCandidates := command_components.BlobStoreIds(envBlobStore.GetBlobStores())

	groups, classifyFails, planErr := planCapture(args, shadowCandidates)

	var sink capture_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON, output_format.FormatNDJSON:
		sink = capture_sink.NewNDJSON(os.Stdout, os.Stderr)
	default:
		sink = capture_sink.NewTAP(os.Stdout)
	}

	failCount := 0

	// captureLogEntries accumulates one entry per receipt produced.
	// Flushed to $XDG_STATE_HOME/<scope>/captures.log via
	// appendCaptureLog after sink.Finalize. Stays empty if nothing
	// successfully receipted.
	var captureLogEntries []captureLogEntry

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

		var entries []capture_receipt.EntryV1

		for _, root := range group.roots {
			if root.shadowNotice != "" {
				sink.Notice(root.shadowNotice)
			}
			result := root.plugin.CaptureRoot(cutting_garden_plugins.CaptureRootRequest{
				Source:    root.sourceURL,
				RawArg:    root.path,
				BlobStore: blobStore,
				Sink:      sink,
			})
			entries = append(entries, result.Entries...)
			failCount += result.FailCount
		}

		// Collapse Root to "." for single-root groups per RFC 0003
		// §Root Encoding. Multi-root groups keep distinct Root values
		// (the §Multi-Root Receipts case).
		if len(group.roots) == 1 {
			for i := range entries {
				entries[i].Root = "."
			}
		}

		if len(entries) == 0 {
			sink.Notice(fmt.Sprintf(
				"notice: no entries captured for store=%s; receipt skipped",
				quoteEmpty(storeName),
			))
			continue
		}

		// Reuse storeName when non-empty; fall back to the resolved
		// default-store id so consumers can lock the lookup to a real
		// store rather than the active store at restore time
		// (RFC 0003 §Store-Hint Resolution; closes amarbel-llc/cutting-garden#12 option (c)).
		effectiveStoreId := storeName
		if effectiveStoreId == "" {
			effectiveStoreId = envBlobStore.GetDefaultBlobStoreId()
		}

		hint, hintErr := computeStoreHint(blobStore, effectiveStoreId)
		if hintErr != nil {
			sink.Notice(fmt.Sprintf(
				"notice: omitting store-hint for store=%s: %v",
				quoteEmpty(storeName), hintErr,
			))
		}
		receiptID, err := writeReceiptBlob(blobStore, entries, hint)
		if err != nil {
			sink.Failure(
				fmt.Sprintf("(receipt:%s)", quoteEmpty(storeName)),
				err,
			)
			failCount++
			continue
		}

		sink.StoreGroupReceipt(receiptID, len(entries))

		captureLogEntries = append(captureLogEntries, captureLogEntry{
			Ts:        captureLogTimestamp(),
			ReceiptID: receiptID,
			StoreID:   storeName,
			Roots:     rootPaths(group.roots),
		})
	}

	sink.Finalize()

	appendCaptureLog(cgEnvDir, sink, captureLogEntries)

	if failCount > 0 {
		errors.ContextCancelWithBadRequestf(
			req,
			"capture failed entries: %d",
			failCount,
		)
		return
	}
}

type captureRoot struct {
	// path is the original CLI argument as the user typed it. Used
	// for the audit log, sink labels, and shadow detection. The
	// EntryV1.Root field in receipts is set by the plugin from the
	// parsed sourceURL, not from path — so schemeless `./foo` and
	// `file:./foo` produce byte-identical receipts.
	path         string
	plugin       cutting_garden_plugins.CapturePlugin
	sourceURL    *url.URL
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
		plugin, _ := cutting_garden_plugins.ResolveCapture("")
		return []captureGroup{{
			roots: []captureRoot{{
				path:      ".",
				plugin:    plugin,
				sourceURL: &url.URL{Path: "."},
			}},
		}}, nil, nil
	}

	if len(args) == 1 {
		k := classifyArg(args[0])
		switch k.kind {
		case argKindStoreId:
			plugin, _ := cutting_garden_plugins.ResolveCapture("")
			return []captureGroup{{
				storeID:      k.storeID,
				switchNotice: arg_resolver.FormatStoreSwitchNotice(k.storeID),
				roots: []captureRoot{{
					path:      ".",
					plugin:    plugin,
					sourceURL: &url.URL{Path: "."},
				}},
			}}, nil, nil
		case argKindCapture:
			if scopeErr := k.plugin.ValidateSource(k.sourceURL, args[0]); scopeErr != nil {
				// Match the multi-arg loop: validation failures route
				// to classifyFails and surface via the sink. The
				// post-classify "failCount > 0" path in Run produces
				// the cancel message; no synthetic planErr is needed.
				return nil, []classifyFailure{{arg: args[0], err: scopeErr}}, nil
			}
			return []captureGroup{{
				roots: []captureRoot{{
					path:         args[0],
					plugin:       k.plugin,
					sourceURL:    k.sourceURL,
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

		case argKindCapture:
			if scopeErr := k.plugin.ValidateSource(k.sourceURL, arg); scopeErr != nil {
				classifyFails = append(classifyFails, classifyFailure{
					arg: arg,
					err: scopeErr,
				})
				continue
			}
			current.roots = append(current.roots, captureRoot{
				path:         arg,
				plugin:       k.plugin,
				sourceURL:    k.sourceURL,
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
	argKindCapture
	argKindStoreId
)

type classifiedArg struct {
	kind      argKind
	storeID   blob_store_id.Id
	plugin    cutting_garden_plugins.CapturePlugin
	sourceURL *url.URL
	err       error
}

// classifyArg decides whether arg names a capture-source URI, a
// blob-store-id, or is unparseable. The schemeless heuristic mirrors
// the pre-plugin behavior: try Lstat first (so a symlink-to-directory
// is rejected — filepath.WalkDir would refuse to descend it anyway,
// and the resulting one-entry "type=symlink" receipt would surprise
// a user who expected the linked tree's contents); on ENOENT, fall
// back to blob-store-id parsing. Users who want symlink-to-dir
// behavior should resolve the symlink with realpath before passing
// it in.
//
// URI args (anything with a scheme that resolves to a registered
// capture plugin) skip the Lstat dance entirely. Args with an
// unrecognized scheme fall through to the schemeless heuristic so
// filenames containing colons (e.g. `myfile:txt`) keep working —
// at the cost of a real edge case: a directory literally named
// `file:foo` is interpreted as the file plugin pointing at `foo`,
// not as the local directory.
func classifyArg(arg string) classifiedArg {
	if u, err := url.Parse(arg); err == nil && u.Scheme != "" {
		if plugin, perr := cutting_garden_plugins.ResolveCapture(u.Scheme); perr == nil {
			return classifiedArg{
				kind:      argKindCapture,
				plugin:    plugin,
				sourceURL: u,
			}
		}
		// Unknown scheme — fall through to the schemeless heuristic.
	}

	info, err := os.Lstat(arg)
	switch {
	case err == nil && info.IsDir():
		plugin, _ := cutting_garden_plugins.ResolveCapture("")
		return classifiedArg{
			kind:      argKindCapture,
			plugin:    plugin,
			sourceURL: &url.URL{Path: arg},
		}
	case err == nil:
		return classifiedArg{
			kind: argKindError,
			err: errors.ErrorWithStackf(
				"%q exists but is not a directory; capture only takes directories (resolve symlinks with realpath if needed)",
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
			"%q is neither a recognized URI, an existing directory, nor a valid blob-store-id",
			arg,
		),
	}
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

// writeReceiptBlob serializes entries via capture_receipt.Write
// into a new blob in blobStore. The output is deterministic:
// equivalent inputs yield byte-identical receipts and identical blob
// IDs. When hint is non-nil, the receipt's hyphence metadata block
// carries an RFC 0003 store-hint line.
func writeReceiptBlob(
	blobStore blob_stores.BlobStoreInitialized,
	entries []capture_receipt.EntryV1,
	hint *capture_receipt.StoreHint,
) (id string, err error) {
	wc, err := blobStore.MakeBlobWriter(nil)
	if err != nil {
		err = errors.Wrap(err)
		return
	}
	defer errors.DeferredCloser(&err, wc)

	if _, err = capture_receipt.WriteV1WithHint(wc, entries, hint); err != nil {
		err = errors.Wrap(err)
		return
	}

	id = wc.GetMarklId().String()
	return
}

// computeStoreHint builds the RFC 0003 store-hint metadata for a
// receipt. storeIdString is the resolved id of the destination store
// (the default-store case is resolved to its actual id by the caller
// per amarbel-llc/cutting-garden#12 option (c), not bypassed here). An empty string is the
// "still couldn't determine an id" sentinel — returns (nil, nil), the
// MAY-omit path RFC 0003 §Producer Rules §Receipt Metadata: Store Hint
// permits.
//
// Returns a non-nil error when the hint should have been computable
// but failed; callers MAY treat that as a soft failure.
//
// The hint pairs storeIdString with the markl-id of the store's
// blob_store-config blob, computed in the store's default hash family
// so consumers can validate it under the same hash the store
// publishes blobs under.
func computeStoreHint(
	blobStore blob_stores.BlobStoreInitialized,
	storeIdString string,
) (*capture_receipt.StoreHint, error) {
	if storeIdString == "" {
		return nil, nil
	}

	cfg := blobStore.BlobStore.GetBlobStoreConfig()
	if cfg == nil {
		return nil, nil
	}

	hashFormat := blobStore.BlobStore.GetDefaultHashType()
	if hashFormat == nil {
		return nil, nil
	}

	hash, _ := hashFormat.GetHash() //repool:owned
	digester := markl_io.MakeWriter(hash, nil)

	typedCfg := &hyphence.TypedBlob[blob_store_configs.Config]{
		Type: blob_store_configs.TypeStructForConfig(cfg),
		Blob: cfg,
	}

	if _, err := blob_store_configs.Coder.EncodeTo(typedCfg, digester); err != nil {
		return nil, errors.Wrap(err)
	}

	return &capture_receipt.StoreHint{
		StoreId:       storeIdString,
		ConfigMarklId: digester.GetMarklId().String(),
	}, nil
}

func quoteEmpty(s string) string {
	if s == "" {
		return `(default)`
	}
	return s
}
