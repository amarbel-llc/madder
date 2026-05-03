package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	tap "github.com/amarbel-llc/tap/go"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/madder/go/internal/hotel/blob_transfers"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
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
			"blobs transferred. Output defaults to TAP on an interactive " +
			"terminal and to NDJSON when stdout is piped; pass -format to " +
			"force a specific encoding. Each JSON record has fields \"id\", " +
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
	// notice reports informational events (limit-hit, summary); stderr in
	// JSON mode.
	notice(msg string)
	// bailOut reports a fatal error that stops the stream.
	bailOut(msg string)
	// finalize flushes. TAP emits the plan; JSON is a no-op.
	finalize()
}

type syncTapSink struct {
	tw *tap.Writer
}

func (s *syncTapSink) transferred(id domain_interfaces.MarklId, bytesWritten int64) {
	s.tw.Ok(formatSyncTestPoint(id, bytesWritten))
}

func (s *syncTapSink) exists(id domain_interfaces.MarklId) {
	s.tw.Ok(formatSyncTestPoint(id, 0))
}

func (s *syncTapSink) failed(id domain_interfaces.MarklId, bytesWritten int64, err error) {
	s.tw.NotOk(formatSyncTestPoint(id, bytesWritten), tap_diagnostics.FromError(err))
}

func (s *syncTapSink) listError(err error) {
	s.tw.NotOk("(unknown blob)", tap_diagnostics.FromError(err))
}

func (s *syncTapSink) notice(msg string) {
	s.tw.Comment(msg)
}

func (s *syncTapSink) bailOut(msg string) {
	s.tw.BailOut(msg)
}

func (s *syncTapSink) finalize() {
	s.tw.Plan()
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
	var sink syncSink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		buf := bufio.NewWriter(os.Stdout)
		sink = &syncJsonSink{
			buf:    buf,
			enc:    json.NewEncoder(buf),
			errOut: os.Stderr,
		}
	default:
		sink = &syncTapSink{tw: tap.NewWriter(os.Stdout)}
	}

	if len(destination) == 0 {
		sink.bailOut("only one blob store, nothing to sync")

		errors.ContextCancelWithBadRequestf(
			req,
			"only one blob store, nothing to sync",
		)

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
			sink.notice(fmt.Sprintf(
				"Successes: %d, Failures: %d, Ignored: %d, Total: %d",
				blobImporter.Counts.Succeeded,
				blobImporter.Counts.Failed,
				blobImporter.Counts.Ignored,
				blobImporter.Counts.Total,
			))

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
			if env_dir.IsErrBlobAlreadyExists(err) {
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
