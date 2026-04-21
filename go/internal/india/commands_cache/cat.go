package commands_cache

import (
	"io"
	"os/exec"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/arg_resolver"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/golf/command"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/quiter"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/delim_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/script_value"
)

func init() {
	utility.AddCmd("cat", &Cat{})
}

type Cat struct {
	command_components.EnvBlobStore

	Utility   script_value.Utility
	PrefixSha bool
}

var (
	_ interfaces.CommandComponentWriter = (*Cat)(nil)
	_ command.CommandWithParams         = (*Cat)(nil)
)

func (cmd *Cat) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "args",
			Description: "markl IDs to retrieve, or blob store IDs to switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd Cat) GetDescription() command.Description {
	return command.Description{
		Short: "output blob contents by digest",
		Long: "Retrieve and output the raw contents of one or more blobs " +
			"from cache stores identified by their content-addressable digest.",
	}
}

func (cmd *Cat) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Utility, "utility", "")
	flagSet.BoolVar(&cmd.PrefixSha, "prefix-sha", false, "")
}

type blobIdWithReadCloser struct {
	BlobId     domain_interfaces.MarklId
	ReadCloser io.ReadCloser
}

func (cmd Cat) makeBlobWriter(
	envRepo command_components.BlobStoreEnv,
	blobStore blob_stores.BlobStoreInitialized,
) interfaces.FuncIter[blobIdWithReadCloser] {
	if cmd.Utility.IsEmpty() {
		return quiter.MakeSyncSerializer(
			func(readCloser blobIdWithReadCloser) (err error) {
				if err = cmd.copy(envRepo, blobStore, readCloser); err != nil {
					err = errors.Wrap(err)
					return err
				}

				return err
			},
		)
	} else {
		return quiter.MakeSyncSerializer(
			func(readCloser blobIdWithReadCloser) (err error) {
				defer errors.DeferredCloser(&err, readCloser.ReadCloser)

				utility := exec.Command(cmd.Utility.Head(), cmd.Utility.Tail()...)
				utility.Stdin = readCloser.ReadCloser

				var out io.ReadCloser

				if out, err = utility.StdoutPipe(); err != nil {
					err = errors.Wrap(err)
					return err
				}

				if err = utility.Start(); err != nil {
					err = errors.Wrap(err)
					return err
				}

				if err = cmd.copy(
					envRepo,
					blobStore,
					blobIdWithReadCloser{
						BlobId:     readCloser.BlobId,
						ReadCloser: out,
					},
				); err != nil {
					err = errors.Wrap(err)
					return err
				}

				if err = utility.Wait(); err != nil {
					err = errors.Wrap(err)
					return err
				}

				return err
			},
		)
	}
}

func (cmd Cat) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStore := envBlobStore.GetDefaultBlobStore()

	blobWriter := cmd.makeBlobWriter(envBlobStore, blobStore)

	var blobStoreId blob_store_id.Id
	explicitStore := false

	for _, arg := range req.PopArgs() {
		resolved := arg_resolver.Resolve(
			arg,
			arg_resolver.ModeBlobId|arg_resolver.ModeStoreSwitch,
		)

		switch resolved.Kind {
		case arg_resolver.KindBlobId:
			if err := cmd.blob(blobStore, &resolved.BlobId, blobWriter); err != nil {
				if explicitStore {
					ui.Err().Print(err)
					continue
				}

				if err := cmd.blobFromRemainingStores(
					envBlobStore,
					&resolved.BlobId,
				); err != nil {
					ui.Err().Print(err)
				}
			}

		case arg_resolver.KindStoreSwitch:
			blobStoreId = resolved.BlobStoreId
			blobStore = envBlobStore.GetBlobStore(blobStoreId)
			blobWriter = cmd.makeBlobWriter(envBlobStore, blobStore)
			explicitStore = true
			ui.Err().Print(arg_resolver.FormatStoreSwitchNotice(blobStoreId))

		case arg_resolver.KindError:
			ui.Err().Print(resolved.Err)
		}
	}
}

func (cmd Cat) copy(
	envBlobStore command_components.BlobStoreEnv,
	blobStore blob_stores.BlobStoreInitialized,
	readCloser blobIdWithReadCloser,
) (err error) {
	defer errors.DeferredCloser(&err, readCloser.ReadCloser)

	if cmd.PrefixSha {
		if _, err = delim_io.CopyWithPrefixOnDelim(
			'\n',
			readCloser.BlobId.String(),
			envBlobStore.GetUI(),
			readCloser.ReadCloser,
			true,
		); err != nil {
			err = errors.Wrap(err)
			return err
		}
	} else {
		if _, err = io.Copy(
			envBlobStore.GetUIFile(),
			readCloser.ReadCloser,
		); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}

func (cmd Cat) blobFromRemainingStores(
	envBlobStore command_components.BlobStoreEnv,
	blobId domain_interfaces.MarklId,
) (err error) {
	_, remaining := envBlobStore.GetDefaultBlobStoreAndRemaining()

	for _, blobStore := range remaining {
		if !blobStore.HasBlob(blobId) {
			continue
		}

		blobWriter := cmd.makeBlobWriter(envBlobStore, blobStore)

		if err = cmd.blob(blobStore, blobId, blobWriter); err != nil {
			continue
		}

		return nil
	}

	err = errors.Errorf("blob not found in any blob store: %s", blobId)

	return err
}

func (cmd Cat) blob(
	blobStore blob_stores.BlobStoreInitialized,
	blobId domain_interfaces.MarklId,
	blobWriter interfaces.FuncIter[blobIdWithReadCloser],
) (err error) {
	var reader domain_interfaces.BlobReader

	if reader, err = blobStore.MakeBlobReader(blobId); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = blobWriter(blobIdWithReadCloser{BlobId: blobId, ReadCloser: reader}); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}
