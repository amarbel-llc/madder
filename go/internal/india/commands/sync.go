package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/crap/go-crap/v2/crap"
	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
	"github.com/amarbel-llc/crap/go-crap/v2/viewport"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/madder/go/internal/hotel/blob_transfers"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("sync", &Sync{
		Format: output_format.Default,
	})
}

type Sync struct {
	command_components.EnvBlobStore
	command_components.BlobStore

	AllowRehashing bool
	Format         output_format.Format
	Limit          int
}

var (
	_ interfaces.CommandComponentWriter = (*Sync)(nil)
	_ futility.CommandWithParams        = (*Sync)(nil)
)

func (cmd *Sync) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-ids",
			Description: "source blob-store-id followed by destination blob-store-ids (defaults to all)",
			Variadic:    true,
		},
	}
}

func (cmd Sync) GetDescription() futility.Description {
	return futility.Description{
		Short: "synchronize blobs between stores",
		Long: "Copy blobs from a source blob store to one or more destination " +
			"stores. The first blob-store-id argument is the source; " +
			"remaining arguments are destinations. Blob-store-ids support " +
			"optional prefixes that select the XDG scope ('.', '/', '%', " +
			"'_', or none) — see blob-store(7). With no arguments, the " +
			"default store is the source and all other configured stores are " +
			"destinations. When source and destination use different hash " +
			"types, blobs are rehashed (source digests are not preserved in " +
			"single-hash destinations). Use -limit to cap the number of " +
			"blobs transferred. On an interactive terminal sync renders a " +
			"live viewport (the in-process form of piping ndjson-crap to " +
			"crap-present); when stdout is piped it emits ndjson-crap. " +
			"Pass -format ndjson (or json) for the legacy per-record JSON " +
			"described below, or -format crap for raw ndjson-crap even on " +
			"a terminal. (-format tap is not supported by sync.) Each JSON " +
			"record has fields \"id\", " +
			"\"state\" (transferred, failed, list_error), \"size\" " +
			"for transferred blobs, and \"error\" for failures. Summary and " +
			"limit notices route to stderr in JSON mode.",
	}
}

func (cmd *Sync) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(
		&cmd.AllowRehashing,
		"allow-rehashing",
		false,
		"allow syncing to stores with a different hash type (source digests not preserved in single-hash destinations)",
	)

	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)

	flagSet.IntVar(
		&cmd.Limit,
		"limit",
		0,
		"number of blobs to sync before stopping. 0 means don't stop (full consent)",
	)
}

func (cmd Sync) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd Sync) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	source, destinations := cmd.MakeSourceAndDestinationBlobStoresFromIdsOrAll(
		req,
		envBlobStore,
	)

	cmd.runStore(req, envBlobStore, source, destinations)
}

// syncSink is the legacy -format ndjson/json wire sink (the crap/viewport
// path uses crap.Reporter directly, not this interface). Already-present
// blobs are reported via transferred with bytesWritten 0 — the legacy
// records do not distinguish a skip from a copy (the crap path does, via
// op.Skip). That fold is intentional and byte-identical to pre-rewrite
// output; see streamToSink.
type syncSink interface {
	// transferred reports a blob copied from source to destinations with
	// bytesWritten known.
	transferred(id domain_interfaces.MarklId, bytesWritten int64)
	// failed reports a transfer failure for a known id.
	failed(id domain_interfaces.MarklId, bytesWritten int64, err error)
	// listError reports a failure reading the source's blob list (no id).
	listError(err error)
	// notice reports informational events (limit-hit); stderr in JSON mode.
	notice(msg string)
	// summary reports final counts. JSON prints a human line to stderr;
	// crap emits the Summary record.
	summary(succeeded, failed, ignored, total int)
	// bailOut reports a fatal error that stops the stream.
	bailOut(msg string)
	// finalize flushes buffered output. crap relies on summary() having
	// been called first.
	finalize()
}

type syncRecord struct {
	Id    string `json:"id,omitempty"`
	Size  *int64 `json:"size,omitempty"`
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
}

const (
	syncStateTransferred = "transferred"
	syncStateExists      = "exists"
	syncStateFailed      = "failed"
	syncStateListError   = "list_error"
	syncStateBailOut     = "bail_out"
)

type syncJsonSink struct {
	buf    *bufio.Writer
	enc    *json.Encoder
	errOut io.Writer
}

func (s *syncJsonSink) emit(rec syncRecord) {
	_ = s.enc.Encode(rec)
}

func (s *syncJsonSink) transferred(id domain_interfaces.MarklId, bytesWritten int64) {
	size := bytesWritten
	s.emit(syncRecord{Id: id.String(), Size: &size, State: syncStateTransferred})
}

func (s *syncJsonSink) failed(id domain_interfaces.MarklId, bytesWritten int64, err error) {
	rec := syncRecord{Id: id.String(), State: syncStateFailed, Error: err.Error()}
	if bytesWritten > 0 {
		size := bytesWritten
		rec.Size = &size
	}
	s.emit(rec)
}

func (s *syncJsonSink) listError(err error) {
	s.emit(syncRecord{State: syncStateListError, Error: err.Error()})
}

func (s *syncJsonSink) notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *syncJsonSink) summary(succeeded, failed, ignored, total int) {
	fmt.Fprintf(s.errOut, "Successes: %d, Failures: %d, Ignored: %d, Total: %d\n",
		succeeded, failed, ignored, total)
}

func (s *syncJsonSink) bailOut(msg string) {
	s.emit(syncRecord{State: syncStateBailOut, Error: msg})
}

func (s *syncJsonSink) finalize() {
	_ = s.buf.Flush()
}

func (cmd Sync) runStore(
	req futility.Request,
	envBlobStore command_components.BlobStoreEnv,
	source blob_stores.BlobStoreInitialized,
	destination blob_stores.BlobStoreMap,
) {
	// The no-destinations bailOut and the cross-hash confirm both run
	// before any streaming begins and short-circuit with a cancelled
	// request, so they do not depend on the viewport-vs-wire choice. The
	// bailOut record is routed through whatever output the format implies
	// (a fresh sink for the wire/JSON paths, the viewport's pipe on a TTY)
	// so the observable record stream is unchanged from before.
	if len(destination) == 0 {
		cmd.emitBailOut(req, "only one blob store, nothing to sync")
		return
	}

	sourceHashType := source.GetDefaultHashType()
	useDestinationHashType := false

	for _, dst := range destination {
		dstHashType := dst.GetDefaultHashType()

		if sourceHashType.GetMarklFormatId() == dstHashType.GetMarklFormatId() {
			continue
		}

		_, isAdder := dst.GetBlobStore().(domain_interfaces.BlobForeignDigestAdder)

		if !isAdder && !cmd.AllowRehashing {
			if !envBlobStore.Confirm(
				fmt.Sprintf(
					"Destination %q uses %s but source uses %s. Rehashing will not preserve source digests. Continue?",
					dst.GetId(),
					dstHashType.GetMarklFormatId(),
					sourceHashType.GetMarklFormatId(),
				),
				"",
			) {
				errors.ContextCancelWithBadRequestf(
					req,
					"cross-hash sync refused: destination %q uses %s, source uses %s. Use -allow-rehashing to skip this check",
					dst.GetId(),
					dstHashType.GetMarklFormatId(),
					sourceHashType.GetMarklFormatId(),
				)

				return
			}
		}

		useDestinationHashType = true
	}

	// auto + TTY renders the live go-crap viewport natively (the
	// in-process form of `madder sync | crap-present`); no wire bytes hit
	// stdout in this mode. The producer is runStoreCrap driving a
	// crap.Reporter into the pipe.
	if cmd.Format == output_format.FormatAuto && output_format.IsTTY(os.Stdout) {
		cmd.streamToViewport(req, envBlobStore, source, destination, useDestinationHashType)
		return
	}

	resolved := cmd.Format
	if resolved == output_format.FormatAuto {
		resolved = output_format.FormatCRAP // piped default
	}

	switch resolved {
	case output_format.FormatCRAP:
		// Raw ndjson-crap operation-family wire (even on a TTY when
		// -format crap is explicit).
		cmd.runStoreCrap(
			req,
			envBlobStore,
			source,
			destination,
			os.Stdout,
			useDestinationHashType,
		)
	case output_format.FormatJSON, output_format.FormatNDJSON:
		buf := bufio.NewWriter(os.Stdout)
		sink := &syncJsonSink{
			buf:    buf,
			enc:    json.NewEncoder(buf),
			errOut: os.Stderr,
		}
		cmd.streamToSink(req, envBlobStore, source, destination, useDestinationHashType, sink)
	default: // tap or anything else sync no longer supports
		errors.ContextCancelWithBadRequestf(
			req,
			"sync does not support -format %s; omit -format for the live viewport on a terminal, or use -format crap, ndjson, or json",
			resolved,
		)
		return
	}
}

// runStoreCrap drives a crap.Reporter (go-crap v2 operation API, crap
// RFC 0001) over out: a coarse scan phase (surfacing the source's lazy
// remote connect + index walk), then a single Operation with one
// Item/Skip/Fail per blob and a tallied operation_end at Finish. The
// viewport consumer renders this as a capped rolling blob list + an
// item-count progress bar + a running byte counter (the driver accumulates
// each Item's bytes into OperationProgress automatically, so no explicit
// Progress calls are needed) + dimmed skip lines.
//
// Materialization strategy: two-pass. Blob-store AllBlobs iterators reuse a
// single pooled MarklId buffer across yields (see localAllBlobs /
// remoteSftp.allBlobsForBase), so retaining yielded ids in a slice would
// alias them all to the last value. Rather than clone every id (markl.Clone
// returns a repool func, awkward to thread across a retained slice), pass 1
// walks AllBlobs counting only — yielding the Operation's Total — and pass 2
// re-walks AllBlobs transferring each blob. The cost is a second source walk
// (a second remote SFTP walk for SFTP sources); the benefit is no id-lifetime
// footgun. A scan-phase list error short-circuits before the Operation opens,
// so no operation is left dangling.
func (cmd Sync) runStoreCrap(
	req futility.Request,
	envBlobStore command_components.BlobStoreEnv,
	source blob_stores.BlobStoreInitialized,
	destination blob_stores.BlobStoreMap,
	out io.Writer,
	useDestinationHashType bool,
) {
	reporter := crap.NewReporter(out, crap.ReporterOptions{Source: "madder"})

	// Stage 1: coarse connect/scan phase. The AllBlobs walk triggers the
	// SFTP connect (lazy initializeOnce), so this phase makes remote
	// bootstrap + the index walk visible coarsely. Pass 1 counts only.
	scan := reporter.Phase(fmt.Sprintf("scanning %s", source.GetId()))

	var total int
	var scanErr error

	for _, errIter := range source.AllBlobs() {
		if errIter != nil {
			scanErr = errIter
			break
		}

		total++
	}

	if scanErr != nil {
		scan.FailDiag(scanErr, syncScanDiagnostic(
			source.GetId().String(),
			source.Config.Blob,
		))
		errors.ContextCancelWithError(req, scanErr)
		return
	}

	scan.Done()

	// Stage 2: the transfer operation. Total = item count from the scan;
	// BytesTotal stays 0 (unknown — a running byte counter, not a
	// determinate byte bar, per the agreed design). Pass 2 re-walks and
	// transfers.
	op := reporter.Operation("sync", crap.OpOptions{Total: total})

	blobImporter := blob_transfers.MakeBlobImporter(
		envBlobStore,
		source,
		destination,
	)

	blobImporter.UseDestinationHashType = useDestinationHashType

	var lastBytesWritten int64

	blobImporter.CopierDelegate = func(result blob_stores.CopyResult) error {
		bytesWritten, _ := result.GetBytesWrittenAndState()
		lastBytesWritten = bytesWritten
		return nil
	}

	for blobId, errIter := range source.AllBlobs() {
		lastBytesWritten = 0

		if errIter != nil {
			// A list error mid-transfer (rare — the scan pass already
			// succeeded) surfaces as a failed item with no id.
			op.Fail("(unknown blob)", errIter)
			continue
		}

		if err := blobImporter.ImportBlobIfNecessary(blobId); err != nil {
			if blob_io.IsErrBlobAlreadyExists(err) {
				op.Skip(formatSyncTestPoint(blobId, 0), syncStateExists)
			} else {
				op.Fail(formatSyncTestPoint(blobId, lastBytesWritten), err)
			}
		} else {
			op.Item(formatSyncTestPoint(blobId, lastBytesWritten), lastBytesWritten)
		}

		if cmd.Limit > 0 &&
			(blobImporter.Counts.Succeeded+blobImporter.Counts.Failed) > cmd.Limit {
			break
		}
	}

	op.Finish()

	if err := reporter.Err(); err != nil {
		errors.ContextCancelWithError(req, err)
	}
}

// syncScanDiagnosticKeys is the connection-identifying subset of
// blob_store_configs.ConfigKeyValues keys that rides a failed scan phase's
// node_end diagnostic: where the scan was pointed, not how the store is
// tuned. Local filesystem paths (private-key-path, tls-*-path) and tuning
// knobs (delta.*, hash_buckets, compression) stay off the wire — the
// stream may be captured into world-readable artifacts.
var syncScanDiagnosticKeys = []string{
	"blob-store-type",
	"host", "port", "user", "remote-path", // sftp
	"url",                                    // webdav
	"endpoint", "region", "bucket", "prefix", // s3
}

// syncScanDiagnostic builds the diagnostic for a failed scan phase
// (#237; go-crap crap#22): source store id plus the
// connection-identifying config keys, making the scan node_end a
// self-sufficient verdict unit on the wire and in the viewport. The keys
// come from the redaction-pinned ConfigKeyValues surface and are further
// narrowed by syncScanDiagnosticKeys.
func syncScanDiagnostic(
	sourceId string,
	config blob_store_configs.Config,
) map[string]any {
	diag := map[string]any{"source": sourceId}

	// A scan failure must never escalate into a panic while being
	// reported; a store with no decoded config still gets the id.
	if config == nil {
		return diag
	}

	keyValues := blob_store_configs.ConfigKeyValues(config)

	for _, key := range syncScanDiagnosticKeys {
		if value, ok := keyValues[key]; ok {
			diag[key] = value
		}
	}

	return diag
}

// emitBailOut writes the no-destinations bailOut record through whatever
// output the resolved format implies, then cancels the request. On a TTY
// (auto) the record is rendered by the viewport; for the crap wire path a
// Meta + Bailout pair is written to stdout; for the JSON paths the
// syncJsonSink emits its bail_out record; -format tap is rejected with a
// guiding error before any record is written.
func (cmd Sync) emitBailOut(req futility.Request, msg string) {
	if cmd.Format == output_format.FormatAuto && output_format.IsTTY(os.Stdout) {
		pr, pw := io.Pipe()
		presentDone := make(chan error, 1)
		go func() {
			presentDone <- viewport.Present(pr, viewport.Options{
				Title: "sync",
				Out:   os.Stdout,
				IsTTY: true,
			})
		}()

		writeSyncBailout(pw, msg)
		_ = pw.Close()
		if presErr := <-presentDone; presErr != nil {
			errors.ContextCancelWithError(req, presErr)
			return
		}

		errors.ContextCancelWithBadRequestf(req, "%s", msg)
		return
	}

	resolved := cmd.Format
	if resolved == output_format.FormatAuto {
		resolved = output_format.FormatCRAP
	}

	switch resolved {
	case output_format.FormatCRAP:
		writeSyncBailout(os.Stdout, msg)
	case output_format.FormatJSON, output_format.FormatNDJSON:
		buf := bufio.NewWriter(os.Stdout)
		sink := &syncJsonSink{buf: buf, enc: json.NewEncoder(buf), errOut: os.Stderr}
		sink.bailOut(msg)
		sink.finalize()
	default: // tap or anything else sync no longer supports
		errors.ContextCancelWithBadRequestf(
			req,
			"sync does not support -format %s; omit -format for the live viewport on a terminal, or use -format crap, ndjson, or json",
			resolved,
		)
		return
	}

	errors.ContextCancelWithBadRequestf(req, "%s", msg)
}

// writeSyncBailout emits a Meta header + Bailout record onto out as
// ndjson-crap. Used by the no-destinations short-circuit for the crap wire
// and viewport paths, where there is no Operation to dangle — a bailout is
// the conformant way to terminate the stream before any work begins.
func writeSyncBailout(out io.Writer, msg string) {
	buf := bufio.NewWriter(out)
	w := ndjsoncrap.NewWriter(buf)
	_ = w.Write(ndjsoncrap.Meta{Title: "sync", Source: "madder"})
	_ = w.Write(ndjsoncrap.Bailout{Message: msg})
	_ = buf.Flush()
}

// streamToViewport runs the producer (runStoreCrap) on the main goroutine
// (so all req interaction stays single-threaded) while viewport.Present
// consumes the ndjson-crap records over an io.Pipe in a goroutine and
// renders the live TUI to stdout. runStoreCrap emits the operation_end into
// the pipe before returning, after which closing the writer gives Present
// EOF so it returns. The go-crap v2.1.0 viewport-driver deadlock fix
// (crap#20) is in this dependency, so the pipe yields a clean EOF.
func (cmd Sync) streamToViewport(
	req futility.Request,
	envBlobStore command_components.BlobStoreEnv,
	source blob_stores.BlobStoreInitialized,
	destination blob_stores.BlobStoreMap,
	useDestinationHashType bool,
) {
	pr, pw := io.Pipe()

	presentDone := make(chan error, 1)
	go func() {
		presentDone <- viewport.Present(pr, viewport.Options{
			Title: "sync",
			Out:   os.Stdout,
			IsTTY: true,
		})
	}()

	// The producer runs on this (main) goroutine, writing operation-family
	// records into the pipe; the viewport renders them live.
	cmd.runStoreCrap(req, envBlobStore, source, destination, pw, useDestinationHashType)

	// runStoreCrap has flushed the operation_end into the pipe by now;
	// closing the writer signals EOF so Present returns.
	_ = pw.Close()

	if err := <-presentDone; err != nil {
		errors.ContextCancelWithError(req, err)
	}
}

// streamToSink drives the importer and the blob loop, emitting per-blob
// events into sink. The deferred block emits the final summary and
// finalizes (flushes) the sink — this is where the viewport's pipe gets
// its terminating Summary record.
func (cmd Sync) streamToSink(
	req futility.Request,
	envBlobStore command_components.BlobStoreEnv,
	source blob_stores.BlobStoreInitialized,
	destination blob_stores.BlobStoreMap,
	useDestinationHashType bool,
	sink syncSink,
) {
	blobImporter := blob_transfers.MakeBlobImporter(
		envBlobStore,
		source,
		destination,
	)

	blobImporter.UseDestinationHashType = useDestinationHashType

	var lastBytesWritten int64

	blobImporter.CopierDelegate = func(result blob_stores.CopyResult) error {
		bytesWritten, _ := result.GetBytesWrittenAndState()
		lastBytesWritten = bytesWritten
		return nil
	}

	defer req.Must(
		func(_ interfaces.ActiveContext) error {
			sink.summary(
				blobImporter.Counts.Succeeded,
				blobImporter.Counts.Failed,
				blobImporter.Counts.Ignored,
				blobImporter.Counts.Total,
			)

			sink.finalize()

			return nil
		},
	)

	for blobId, errIter := range source.AllBlobs() {
		lastBytesWritten = 0

		if errIter != nil {
			sink.listError(errIter)
			continue
		}

		if err := blobImporter.ImportBlobIfNecessary(blobId); err != nil {
			if blob_io.IsErrBlobAlreadyExists(err) {
				// Legacy ndjson/json: an already-present blob folds into
				// transferred (bytesWritten 0), not a distinct skip — kept
				// byte-identical to pre-rewrite output. The crap path
				// distinguishes it via op.Skip.
				sink.transferred(blobId, lastBytesWritten)
			} else {
				sink.failed(blobId, lastBytesWritten, err)
			}
		} else {
			sink.transferred(blobId, lastBytesWritten)
		}

		if cmd.Limit > 0 &&
			(blobImporter.Counts.Succeeded+blobImporter.Counts.Failed) > cmd.Limit {
			sink.notice("limit hit, stopping")
			break
		}
	}
}

func formatSyncTestPoint(
	blobId domain_interfaces.MarklId,
	bytesWritten int64,
) string {
	if bytesWritten > 0 {
		return fmt.Sprintf("%s (%s)", blobId, ui.GetHumanBytesStringOrError(bytesWritten))
	}

	return fmt.Sprintf("%s", blobId)
}
