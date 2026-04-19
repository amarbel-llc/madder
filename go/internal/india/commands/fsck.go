package commands

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/amarbel-llc/madder/go/internal/charlie/blob_verify_sink"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
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
}

var _ command.CommandWithParams = (*Fsck)(nil)

func (cmd *Fsck) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "store-ids",
			Description: "blob store IDs to verify (defaults to all configured stores)",
			Variadic:    true,
		},
	}
}

func (cmd Fsck) GetDescription() command.Description {
	return command.Description{
		Short: "verify blob store integrity",
		Long: "Verify the integrity of one or more blob stores by reading " +
			"every blob and recomputing its content-addressable digest. " +
			"Reports corrupt, missing, or unreadable blobs. With no " +
			"arguments, all configured stores are checked. Pass store IDs " +
			"to check specific stores. Output defaults to TAP on an " +
			"interactive terminal and to NDJSON when stdout is piped; pass " +
			"-format to force a specific encoding. Each JSON record has " +
			"fields \"id\" (for per-blob events), \"store\", \"state\" " +
			"(verified, missing, corrupt, read_error, bail_out), and " +
			"\"error\" on failures. Progress ticks and summaries route to " +
			"stderr in JSON mode.",
	}
}

func (cmd *Fsck) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

func (cmd Fsck) Complete(
	req command.Request,
	envLocal env_local.Env,
	commandLine command.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	for id, blobStore := range envBlobStore.GetBlobStores() {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd Fsck) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	blobStores := cmd.MakeBlobStoresFromIdsOrAll(req, envBlobStore)

	var sink blob_verify_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		sink = blob_verify_sink.NewJSON(os.Stdout, os.Stderr)
	default:
		sink = blob_verify_sink.NewTAP(os.Stdout)
	}

	for storeId, blobStore := range blobStores {
		sink.Notice(fmt.Sprintf("(blob_store: %s) starting fsck...", storeId))

		var count atomic.Uint32
		var errorCount atomic.Uint32
		var progressWriter env_ui.ProgressWriter

		if err := errors.RunChildContextWithPrintTicker(
			envBlobStore,
			func(ctx errors.Context) {
				for digest, err := range blobStore.AllBlobs() {
					errors.ContextContinueOrPanic(ctx)

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
						sink.Corrupt(digest, storeId, err)
						errorCount.Add(1)

						continue
					}

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

		sink.Notice(fmt.Sprintf(
			"(blob_store: %s) blobs verified: %d, bytes verified: %s",
			storeId,
			count.Load(),
			progressWriter.GetWrittenHumanString(),
		))
	}

	sink.Finalize()
}
