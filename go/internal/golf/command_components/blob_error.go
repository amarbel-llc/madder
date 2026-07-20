package command_components

import (
	"fmt"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/collections_slice"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/pool"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
)

type BlobError struct {
	BlobId domain_interfaces.MarklId
	Err    error
}

func PrintBlobErrors(
	envLocal env_local.Env,
	blobErrors collections_slice.Slice[BlobError],
) {
	// Route the header through the same env err sink as the per-blob
	// lines below — not the process-global stderr printer (#229).
	envLocal.GetErr().Printf("blobs with errors: %d", blobErrors.Len())

	bufferedWriter, repool := pool.GetBufferedWriter(envLocal.GetErr())
	defer repool()

	defer errors.ContextMustFlush(envLocal, bufferedWriter)

	for _, errorBlob := range blobErrors {
		if errorBlob.BlobId == nil {
			bufferedWriter.WriteString("(empty blob id): ")
		} else {
			fmt.Fprintf(bufferedWriter, "%s: ", errorBlob.BlobId)
		}

		ui.CLIErrorTreeEncoder.EncodeTo(errorBlob.Err, bufferedWriter)

		bufferedWriter.WriteByte('\n')
	}
}
