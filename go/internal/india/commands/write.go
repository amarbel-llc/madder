package commands

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/charlie/blob_write_sink"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
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
		Short: "write blobs to a store",
		Long: "Write files or stdin into the content-addressable blob store. " +
			"Each argument is a file path, '-' for stdin, or a blob store ID " +
			"that switches the active store for subsequent writes. Store IDs " +
			"support optional prefixes that select the XDG scope: '.' for " +
			"CWD-relative, '/' for system-wide, '%' for cache, '_' for " +
			"custom-rooted, and no prefix for the user default — see " +
			"blob-store(7). Unprefixed names are resolved as files first; " +
			"to target a store that shares a name with a file in CWD use " +
			"an explicit prefix (e.g. '~mystore', '_mystore'). Output " +
			"defaults to TAP on an interactive terminal and to NDJSON " +
			"(one JSON object per blob, suitable for programmatic consumers) " +
			"when stdout is piped; pass -format to force a specific encoding. " +
			"Each JSON record has fields \"id\", \"size\", and \"source\", plus " +
			"\"store\" when a non-default store is active, \"present\" under " +
			"-check, \"error\" on per-arg failures, and \"skipped\" for null " +
			"digests. Use -check to verify that files already exist in the " +
			"store without writing them.",
	}
}

func (cmd Write) Complete(
	req command.Request,
	envLocal env_local.Env,
	commandLine command.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	// args := commandLine.FlagsOrArgs[1:]

	// if commandLine.InProgress != "" {
	// 	args = args[:len(args)-1]
	// }

	for id, blobStore := range blobStores {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
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

		resolved := command_components.ResolveFileOrBlobStoreId(arg)

		if resolved.Err != nil {
			sink.Failure(arg, resolved.Err)
			failCount.Add(1)
			continue
		}

		if resolved.IsStoreSwitch {
			blobStoreId = resolved.BlobStoreId
			blobStore = envBlobStore.GetBlobStore(blobStoreId)
			sink.Notice(fmt.Sprintf("switched to blob store: %s", blobStoreId))
			continue
		}

		// The arg resolved to a file. If any configured blob store shares
		// the arg's bare name (regardless of XDG scope), the caller
		// probably intended the store-switch — warn them and point at the
		// disambiguating forms. Prefixed names (/, ~, ., _, %) bypass the
		// filesystem lookup entirely so this only fires for unprefixed
		// args.
		var shadowedId blob_store_id.Id
		if err := shadowedId.Set(arg); err == nil {
			for _, store := range envBlobStore.GetBlobStores() {
				if store.Path.GetId().GetName() == shadowedId.GetName() {
					sink.Notice(fmt.Sprintf(
						"warning: %q shadows blob store %q; use './%s' for the file or %q for the store",
						arg, store.Path.GetId(), arg, store.Path.GetId().String(),
					))
					break
				}
			}
		}

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
