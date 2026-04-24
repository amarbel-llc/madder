package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/charlie/blob_write_sink"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("pack-blobs", &PackBlobs{
		Format: output_format.Default,
	})
}

type PackBlobs struct {
	command_components.EnvBlobStore

	DeleteLoose bool
	Format      output_format.Format
	MaxPackSize ui.HumanReadableBytes
	Delta       bool
}

var (
	_ interfaces.CommandComponentWriter = (*PackBlobs)(nil)
	_ futility.CommandWithParams        = (*PackBlobs)(nil)
)

func (cmd *PackBlobs) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "args",
			Description: "file paths, '-' for stdin, or blob-store-ids to switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd PackBlobs) GetDescription() futility.Description {
	return futility.Description{
		Short: "write files and pack them into an archive",
		Long: "Write files into the blob store and then pack just those " +
			"blobs into an archive. Arguments are file paths, '-' for " +
			"stdin, or blob-store-ids that switch the active store. " +
			"Store IDs support the same XDG-scope prefixes as 'write' " +
			"('.', '/', '%', '_', or none) — see blob-store(7). " +
			"Unlike 'pack', which packs all loose blobs, this command " +
			"targets only the blobs written from the given files. " +
			"Output defaults to TAP on an interactive terminal and to " +
			"NDJSON when stdout is piped; pass -format to force a specific " +
			"encoding. Per-arg writes share madder-write(1)'s JSON record " +
			"shape; a final {\"state\":\"packed\" or \"pack_failed\",\"store\":\"...\"} " +
			"record closes the stream. In JSON mode, the internal packer's " +
			"phase-level TAP output routes to stderr so stdout stays NDJSON.",
	}
}

func (cmd PackBlobs) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	for id, blobStore := range blobStores {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *PackBlobs) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(&cmd.DeleteLoose, "delete-loose", false,
		"validate archive then delete packed loose blobs")
	flagSet.BoolVar(&cmd.Delta, "delta", false,
		"enable delta compression during packing")
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
	flagSet.Var(&cmd.MaxPackSize, "max-pack-size",
		"override max pack size (e.g. 100M, 1G, 0 = unlimited)",
	)
}

// packFinalRecord is emitted in JSON mode after all args and the pack
// phase complete, so consumers can detect end-of-stream and pack outcome.
type packFinalRecord struct {
	State string `json:"state"`
	Store string `json:"store,omitempty"`
	Error string `json:"error,omitempty"`
}

const (
	packStatePacked = "packed"
	packStateFailed = "pack_failed"
)

func (cmd PackBlobs) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStore := envBlobStore.GetDefaultBlobStore()

	format := cmd.Format.Resolve(os.Stdout)

	// In TAP mode the per-arg sink and the pack internals share one
	// tap.Writer so all test points are numbered consecutively on
	// stdout. In JSON mode the internals write TAP to stderr so stdout
	// stays valid NDJSON.
	var (
		sink            blob_write_sink.Sink
		packTapWriter   *tap.Writer
		emitFinal       func(success bool, err error)
		jsonFinalWriter io.Writer
	)

	switch format {
	case output_format.FormatJSON:
		sink = blob_write_sink.NewJSON(os.Stdout, os.Stderr, false)
		packTapWriter = tap.NewWriter(os.Stderr)
		jsonFinalWriter = os.Stdout
	default:
		tw := tap.NewWriter(os.Stdout)
		sink = blob_write_sink.NewTAPWithWriter(tw)
		packTapWriter = tw
	}

	var blobStoreId blob_store_id.Id
	storeIdString := ".default"
	blobFilter := make(map[string]domain_interfaces.MarklId)

	shadowCandidates := command_components.BlobStoreIds(
		envBlobStore.GetBlobStores(),
	)

	sawStdin := false

	for _, arg := range req.PopArgs() {
		switch {
		case arg == "-" && sawStdin:
			sink.Notice("'-' passed in more than once. Ignoring")
			continue

		case arg == "-":
			sawStdin = true
		}

		resolved := arg_resolver.Resolve(
			arg,
			arg_resolver.ModeFile|arg_resolver.ModeStoreSwitch,
		)

		switch resolved.Kind {
		case arg_resolver.KindError:
			sink.Failure(arg, resolved.Err)
			continue

		case arg_resolver.KindStoreSwitch:
			blobStoreId = resolved.BlobStoreId
			blobStore = envBlobStore.GetBlobStore(blobStoreId)
			storeIdString = blobStoreId.String()
			sink.Notice(arg_resolver.FormatStoreSwitchNotice(blobStoreId))
			continue
		}

		if shadowed, ok := arg_resolver.DetectShadow(arg, shadowCandidates); ok {
			sink.Notice(arg_resolver.FormatShadowWarning(arg, shadowed))
		}

		blobId, size, err := cmd.doOne(blobStore, resolved.BlobReader)
		if err != nil {
			sink.Failure(arg, err)
			continue
		}

		if blobId.IsNull() {
			sink.Skip(arg, "null digest")
			continue
		}

		storeName := ""
		if !blobStoreId.IsEmpty() {
			storeName = blobStoreId.String()
		}

		sink.Success(blobId, size, arg, storeName)
		blobFilter[blobId.String()] = blobId
	}

	if format == output_format.FormatJSON {
		enc := json.NewEncoder(jsonFinalWriter)
		emitFinal = func(success bool, err error) {
			rec := packFinalRecord{Store: storeIdString}
			if success {
				rec.State = packStatePacked
			} else {
				rec.State = packStateFailed
				if err != nil {
					rec.Error = err.Error()
				}
			}
			_ = enc.Encode(rec)
		}
	} else {
		emitFinal = func(success bool, err error) {
			point := fmt.Sprintf("pack %s", storeIdString)
			if success {
				packTapWriter.Ok(point)
				return
			}
			if err == nil {
				err = fmt.Errorf("pack failed")
			}
			packTapWriter.NotOk(point, tap_diagnostics.FromError(err))
		}
	}

	if len(blobFilter) == 0 {
		sink.Finalize()
		return
	}

	packable, ok := blobStore.BlobStore.(blob_stores.PackableArchive)
	if !ok {
		emitFinal(false, fmt.Errorf("not packable"))
		sink.Finalize()
		return
	}

	if err := packable.Pack(blob_stores.PackOptions{
		Context:              req,
		DeleteLoose:          cmd.DeleteLoose,
		DeletionPrecondition: blob_stores.NopDeletionPrecondition(),
		BlobFilter:           blobFilter,
		MaxPackSize:          cmd.MaxPackSize.GetByteCount(),
		Delta:                cmd.Delta,
		TapWriter:            packTapWriter,
	}); err != nil {
		emitFinal(false, err)
		sink.Finalize()
		return
	}

	emitFinal(true, nil)
	sink.Finalize()
}

func (cmd PackBlobs) doOne(
	blobStore blob_stores.BlobStoreInitialized,
	blobReader domain_interfaces.BlobReader,
) (blobId domain_interfaces.MarklId, size int64, err error) {
	defer errors.DeferredCloser(&err, blobReader)

	var writeCloser domain_interfaces.BlobWriter

	if writeCloser, err = blobStore.MakeBlobWriter(nil); err != nil {
		err = errors.Wrap(err)
		return blobId, size, err
	}

	defer errors.DeferredCloser(&err, writeCloser)

	if size, err = io.Copy(writeCloser, blobReader); err != nil {
		err = errors.Wrap(err)
		return blobId, size, err
	}

	blobId = writeCloser.GetMarklId()

	return blobId, size, err
}
