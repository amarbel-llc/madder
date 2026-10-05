package commands

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"code.linenisgreat.com/madder/go/internal/charlie/blob_verify_sink"
	"code.linenisgreat.com/madder/go/internal/charlie/output_format"
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("fsck", &Fsck{
		Format: output_format.Default,
	})
}

type Fsck struct {
	command_components.EnvBlobStore
	command_components.BlobStore

	Format output_format.Format
	Limit  int
}

var _ futility.CommandWithParams = (*Fsck)(nil)

func (cmd *Fsck) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-ids",
			Description: "blob-store-ids to verify (defaults to all configured stores)",
			Variadic:    true,
		},
	}
}

func (cmd Fsck) GetDescription() futility.Description {
	return futility.Description{
		Short: "verify blob store integrity",
		Long: "Verify the integrity of one or more blob stores by reading " +
			"every blob and recomputing its content-addressable digest. " +
			"Reports corrupt, missing, or unreadable blobs. With no " +
			"arguments, all configured stores are checked. Pass " +
			"blob-store-ids " +
			"to check specific stores. Store IDs support optional prefixes " +
			"that select the XDG scope ('.', '/', '%', '_', or none) — " +
			"see blob-store(7). Output defaults to TAP on an interactive " +
			"terminal and to NDJSON when stdout is piped; pass -format to " +
			"force a specific encoding. Each JSON record has fields " +
			"\"id\" (for per-blob events), \"store\", \"state\" (verified, " +
			"missing, corrupt, read_error, bail_out), and \"error\" on " +
			"failures. \"corrupt\" means the stored bytes are bad: a " +
			"digest mismatch, or a blob that will not decrypt or " +
			"decompress. \"read_error\" means the blob could not be " +
			"read for a reason unrelated to its bytes, such as an " +
			"unreachable key agent or blob store, so the blob may be " +
			"intact. fsck exits non-zero if any blob was missing, " +
			"corrupt or unreadable, after reporting on every blob. " +
			"Progress ticks and summaries route to stderr in " +
			"JSON mode. Note that a store holding foreign-digest aliases " +
			"— written by a cross-hash sync into a multi-hash store — " +
			"verifies each aliased blob twice, once under each digest. " +
			"Both verify correctly, since one blob's bytes have a valid " +
			"digest under either hash, but the reported blob and byte " +
			"totals count it twice.",
	}
}

func (cmd *Fsck) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
	flagSet.IntVar(&cmd.Limit, "limit", 0,
		"stop after verifying this many blobs per store (0 = no limit). "+
			"Useful for quickly sanity-checking a large legacy store.")
}

func (cmd Fsck) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd Fsck) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	blobStores := cmd.MakeBlobStoresFromIdsOrAll(req, envBlobStore)

	var sink blob_verify_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON, output_format.FormatNDJSON:
		sink = blob_verify_sink.NewJSON(os.Stdout, os.Stderr)
	default:
		sink = blob_verify_sink.NewTAP(os.Stdout)
	}

	var totalErrors uint32

	for storeId, blobStore := range blobStores {
		sink.Notice(fmt.Sprintf("(blob_store: %s) starting fsck...", storeId))

		var count atomic.Uint32
		var verifiedCount atomic.Uint32
		var errorCount atomic.Uint32
		var progressWriter env_ui.ProgressWriter

		if err := errors.RunChildContextWithPrintTicker(
			envBlobStore,
			func(ctx errors.Context) {
				for digest, err := range blobStore.AllBlobs() {
					errors.ContextContinueOrPanic(ctx)

					if cmd.Limit > 0 && count.Load() >= uint32(cmd.Limit) {
						return
					}

					if err != nil {
						sink.ReadError(storeId, err)
						errorCount.Add(1)
						count.Add(1)

						continue
					}

					count.Add(1)

					if !blobStore.HasBlob(digest) {
						sink.Missing(digest, storeId)
						errorCount.Add(1)

						continue
					}

					if err = blob_stores.VerifyBlob(
						ctx,
						blobStore,
						digest,
						io.MultiWriter(&progressWriter, io.Discard),
					); err != nil {
						if blob_stores.IsErrBlobUnreadable(err) {
							sink.Unreadable(digest, storeId, err)
						} else {
							sink.Corrupt(digest, storeId, err)
						}

						errorCount.Add(1)

						continue
					}

					verifiedCount.Add(1)
					sink.Verified(digest, storeId)
				}
			},
			func(time time.Time) {
				sink.Notice(fmt.Sprintf(
					"(blob_store: %s) %d blobs / %s verified, %d errors",
					storeId,
					count.Load(),
					progressWriter.GetWrittenHumanString(),
					errorCount.Load(),
				))
			},
			3*time.Second,
		); err != nil {
			sink.BailOut(err.Error())
			envBlobStore.Cancel(err)
			return
		}

		sink.Notice(blob_verify_sink.StoreSummary(
			storeId,
			verifiedCount.Load(),
			errorCount.Load(),
			progressWriter.GetWrittenHumanString(),
		))

		totalErrors += errorCount.Load()
	}

	sink.Finalize()

	// The full report is out; now make the exit status agree with it, so a
	// caller checking only the status does not read failures as success
	// (madder#299).
	if totalErrors > 0 {
		errors.ContextCancelWithError(
			req,
			errors.Errorf("fsck: %d blob(s) failed verification", totalErrors),
		)
	}
}
