package commands

import (
	"encoding/json"
	"io"
	"strings"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

func init() {
	utility.AddCmd("read", &Read{})
}

type Read struct {
	command_components.EnvBlobStore
}

var _ futility.CommandWithParams = (*Read)(nil)

func (cmd *Read) GetParams() []futility.Param { return nil }

func (cmd Read) GetDescription() futility.Description {
	return futility.Description{
		Short: "read blobs from JSON on stdin",
		Long: "Read JSON objects from stdin and write each blob value into " +
			"the content-addressable store. Each JSON object must have a " +
			"\"blob\" field containing the content to store. An optional " +
			"\"store\" field switches the target blob store for that entry. " +
			"This command is the programmatic counterpart to 'write', " +
			"accepting structured input rather than file paths.",
	}
}

type readBlobEntry struct {
	Blob  string `json:"blob"`
	Store string `json:"store,omitempty"`
}

func (cmd Read) Run(dep futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(dep)

	decoder := json.NewDecoder(envBlobStore.GetInFile())
	blobStore := envBlobStore.GetDefaultBlobStore()

	for {
		var entry readBlobEntry

		if err := decoder.Decode(&entry); err != nil {
			if errors.IsEOF(err) {
				err = nil
			} else {
				envBlobStore.Cancel(err)
			}

			return
		}

		if entry.Store != "" {
			var storeId scoped_id.Id

			if err := storeId.Set(entry.Store); err != nil {
				envBlobStore.Cancel(err)
				return
			}

			blobStore = envBlobStore.GetBlobStore(storeId)
		}

		{
			var err error

			if _, err = cmd.readOneBlob(blobStore, entry); err != nil {
				envBlobStore.Cancel(err)
			}
		}
	}
}

func (Read) readOneBlob(
	blobStore blob_stores.BlobStoreInitialized,
	entry readBlobEntry,
) (digest domain_interfaces.MarklId, err error) {
	var writeCloser domain_interfaces.BlobWriter

	if writeCloser, err = blobStore.MakeBlobWriter(
		nil,
	); err != nil {
		err = errors.Wrap(err)
		return digest, err
	}

	defer errors.DeferredCloser(&err, writeCloser)

	if _, err = io.Copy(writeCloser, strings.NewReader(entry.Blob)); err != nil {
		err = errors.Wrap(err)
		return digest, err
	}

	digest = writeCloser.GetMarklId()

	return digest, err
}
