package commands_cache

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/charlie/blob_write_sink"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/script_value"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("write", &Write{
		Format: output_format.Default,
	})
}

type Write struct {
	command_components.EnvBlobStore

	Check         bool
	Format        output_format.Format
	UtilityBefore script_value.Utility
	UtilityAfter  script_value.Utility
}

var (
	_ interfaces.CommandComponentWriter = (*Write)(nil)
	_ command.CommandWithParams         = (*Write)(nil)
)

func (cmd *Write) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "args",
			Description: "file paths, '-' for stdin, or blob store IDs to switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd Write) GetDescription() command.Description {
	return command.Description{
		Short: "write blobs to a cache store",
		Long: "Write files or stdin into a purgeable content-addressable " +
			"blob store under XDG_CACHE_HOME. Output defaults to TAP on an " +
			"interactive terminal and to NDJSON when stdout is piped; pass " +
			"-format to force a specific encoding. See madder-write(1) for " +
			"the per-blob JSON record shape.",
	}
}

func (cmd *Write) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(
		&cmd.Check,
		"check",
		false,
		"only check if the object already exists",
	)

	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)

	flagSet.Var(&cmd.UtilityBefore, "utility-before", "")
	flagSet.Var(&cmd.UtilityAfter, "utility-after", "")
}

func (cmd Write) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStore := envBlobStore.GetDefaultBlobStore()

	var sink blob_write_sink.Sink
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		sink = blob_write_sink.NewJSON(os.Stdout, os.Stderr, cmd.Check)
	default:
		sink = blob_write_sink.NewTAP(os.Stdout)
	}

	var failCount atomic.Uint32
	var blobStoreId blob_store_id.Id

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
			failCount.Add(1)
			continue

		case arg_resolver.KindStoreSwitch:
			blobStoreId = resolved.BlobStoreId
			blobStore = envBlobStore.GetBlobStore(blobStoreId)
			sink.Notice(fmt.Sprintf("switched to blob-store-id: %s", blobStoreId))
			continue
		}

		// KindFile — cache-write currently skips the shadow warning
		// because cache stores live under $XDG_CACHE_HOME, not CWD —
		// a file in CWD with the same bare name as a cache store
		// can't reach the cache root by accident. If that assumption
		// ever changes, lift the DetectShadow call from write.go.

		blobId, size, err := cmd.doOne(blobStore, resolved.BlobReader)
		if err != nil {
			sink.Failure(arg, err)
			failCount.Add(1)
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

		hasBlob := blobStore.HasBlob(blobId)

		if hasBlob {
			sink.Success(blobId, size, arg, storeName)
		} else {
			if cmd.Check {
				sink.CheckMiss(blobId, size, arg, storeName)
				failCount.Add(1)
			} else {
				sink.Success(blobId, size, arg, storeName)
			}
		}
	}

	sink.Finalize()

	fc := failCount.Load()

	if fc > 0 {
		errors.ContextCancelWithBadRequestf(
			req,
			"untracked objects: %d",
			fc,
		)
		return
	}
}

func (cmd Write) doOne(
	blobStore blob_stores.BlobStoreInitialized,
	blobReader domain_interfaces.BlobReader,
) (blobId domain_interfaces.MarklId, size int64, err error) {
	defer errors.DeferredCloser(&err, blobReader)

	var writeCloser domain_interfaces.BlobWriter

	if cmd.Check {
		{
			hash, hashRepool := blobStore.GetDefaultHashType().GetHash()
			var repool func()
			writeCloser, repool = markl_io.MakeWriterWithRepool(
				hash,
				nil,
			)
			defer func() { repool(); hashRepool() }()
		}
	} else {
		if writeCloser, err = blobStore.MakeBlobWriter(nil); err != nil {
			err = errors.Wrap(err)
			return blobId, size, err
		}
	}

	defer errors.DeferredCloser(&err, writeCloser)

	if size, err = io.Copy(writeCloser, blobReader); err != nil {
		err = errors.Wrap(err)
		return blobId, size, err
	}

	blobId = writeCloser.GetMarklId()

	return blobId, size, err
}
