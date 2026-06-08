package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/crap/go-crap/ndjsoncrap"
	"github.com/amarbel-llc/crap/go-crap/viewport"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
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
			"\"state\" (transferred, exists, failed, list_error), \"size\" " +
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

// syncSink is Sync-local because the per-blob event shape (transfer with
// byte count + exists + failed) differs from blob_write_sink's record.
type syncSink interface {
	// transferred reports a blob copied from source to destinations with
	// bytesWritten known.
	transferred(id domain_interfaces.MarklId, bytesWritten int64)
	// exists reports a blob skipped because it was already in all
	// destinations (bytesWritten is 0).
	exists(id domain_interfaces.MarklId)
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

func (s *syncJsonSink) exists(id domain_interfaces.MarklId) {
	s.emit(syncRecord{Id: id.String(), State: syncStateExists})
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

// syncCrapSink emits ndjson-crap result-family records (go-crap v2).
// Routing parity note: runStore never calls exists() — already-present
// blobs surface as transferred with size 0 (see the IsErrBlobAlreadyExists
// branch). The exists() impl below is for interface completeness; the
// skip-directive mapping activates if/when runStore routes it.
//
// No Plan record is emitted: sync only learns its count after running,
// so per tap-ndjson(7) the count is reported in the Summary's PlanCount
// instead (the trailing-plan-producer case).
type syncCrapSink struct {
	buf    *bufio.Writer
	w      *ndjsoncrap.Writer
	errOut io.Writer
	n      int
}

func newSyncCrapSink(out io.Writer, errOut io.Writer) *syncCrapSink {
	buf := bufio.NewWriter(out)
	sink := &syncCrapSink{buf: buf, w: ndjsoncrap.NewWriter(buf), errOut: errOut}
	_ = sink.w.Write(ndjsoncrap.Meta{Title: "sync", Source: "madder"})
	return sink
}

func (s *syncCrapSink) test(t ndjsoncrap.Test) {
	s.n++
	t.N = s.n
	_ = s.w.Write(t)
}

func (s *syncCrapSink) transferred(id domain_interfaces.MarklId, bytesWritten int64) {
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, bytesWritten),
		OK:          true,
		Diagnostic:  map[string]any{"state": syncStateTransferred, "size": bytesWritten},
	})
}

func (s *syncCrapSink) exists(id domain_interfaces.MarklId) {
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, 0),
		OK:          true,
		Directive:   &ndjsoncrap.Directive{Kind: "skip", Reason: syncStateExists},
		Diagnostic:  map[string]any{"state": syncStateExists},
	})
}

func (s *syncCrapSink) failed(id domain_interfaces.MarklId, bytesWritten int64, err error) {
	diag := map[string]any{"state": syncStateFailed, "error": err.Error()}
	if bytesWritten > 0 {
		diag["size"] = bytesWritten
	}
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, bytesWritten),
		Diagnostic:  diag,
	})
}

func (s *syncCrapSink) listError(err error) {
	s.test(ndjsoncrap.Test{
		Description: "(unknown blob)",
		Diagnostic:  map[string]any{"state": syncStateListError, "error": err.Error()},
	})
}

func (s *syncCrapSink) notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *syncCrapSink) bailOut(msg string) {
	_ = s.w.Write(ndjsoncrap.Bailout{Message: msg})
}

func (s *syncCrapSink) summary(succeeded, failed, ignored, total int) {
	_ = s.w.Write(ndjsoncrap.Summary{
		Passed:    succeeded,
		Failed:    failed,
		Skipped:   ignored,
		Total:     total,
		PlanCount: total,
		Valid:     true,
	})
}

func (s *syncCrapSink) finalize() {
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
	// stdout in this mode.
	if cmd.Format == output_format.FormatAuto && output_format.IsTTY(os.Stdout) {
		cmd.streamToViewport(req, envBlobStore, source, destination, useDestinationHashType)
		return
	}

	resolved := cmd.Format
	if resolved == output_format.FormatAuto {
		resolved = output_format.FormatCRAP // piped default
	}

	var sink syncSink
	switch resolved {
	case output_format.FormatCRAP:
		sink = newSyncCrapSink(os.Stdout, os.Stderr)
	case output_format.FormatJSON, output_format.FormatNDJSON:
		buf := bufio.NewWriter(os.Stdout)
		sink = &syncJsonSink{
			buf:    buf,
			enc:    json.NewEncoder(buf),
			errOut: os.Stderr,
		}
	default: // tap or anything else sync no longer supports
		errors.ContextCancelWithBadRequestf(
			req,
			"sync does not support -format %s; omit -format for the live viewport on a terminal, or use -format crap, ndjson, or json",
			resolved,
		)
		return
	}

	cmd.streamToSink(req, envBlobStore, source, destination, useDestinationHashType, sink)
}

// emitBailOut writes the no-destinations bailOut record through whatever
// output the resolved format implies, then cancels the request. On a TTY
// (auto) the record is rendered by the viewport; for the wire/JSON paths a
// throwaway sink emits it to stdout; -format tap is rejected with a guiding
// error before any record is written.
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

		sink := newSyncCrapSink(pw, io.Discard)
		sink.bailOut(msg)
		sink.finalize()
		_ = pw.Close()
		<-presentDone

		errors.ContextCancelWithBadRequestf(req, "%s", msg)
		return
	}

	resolved := cmd.Format
	if resolved == output_format.FormatAuto {
		resolved = output_format.FormatCRAP
	}

	switch resolved {
	case output_format.FormatCRAP:
		sink := newSyncCrapSink(os.Stdout, os.Stderr)
		sink.bailOut(msg)
		sink.finalize()
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

// streamToViewport runs the blob loop on the main goroutine (so all req
// interaction stays single-threaded) while viewport.Present consumes the
// ndjson-crap records over an io.Pipe in a goroutine and renders the live
// TUI to stdout. streamToSink's deferred block writes the Summary and
// flushes into the pipe before returning, after which closing the writer
// gives Present EOF so it returns. Records are producer-generated and
// always valid, so Present's decoder cannot error mid-stream — the pipe
// only ever yields a clean EOF, which is why this cannot deadlock.
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

	// Notices are discarded in viewport mode; the Summary record is
	// rendered natively. The producer runs on this (main) goroutine.
	sink := newSyncCrapSink(pw, io.Discard)
	cmd.streamToSink(req, envBlobStore, source, destination, useDestinationHashType, sink)

	// streamToSink's deferred summary+finalize has flushed the Summary
	// into the pipe by now; closing the writer signals EOF so Present
	// returns.
	_ = pw.Close()

	// NOTE(crap#20): viewport.Present can hang if bubbletea's p.Run()
	// errors before draining the pipe — the producer above blocks on a
	// full pipe while Present never reaches its read loop, so this
	// <-presentDone never fires. The fix belongs upstream in
	// viewport.Present, not here. See
	// https://github.com/amarbel-llc/crap/issues/20.
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
