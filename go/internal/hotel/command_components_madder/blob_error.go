package command_components_madder

import (
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/purse-first/libs/dewey/alfa/pool"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/collections_slice"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
)

type BlobError struct {
	BlobId domain_interfaces.MarklId
	Err    error
}

func PrintBlobErrors(
	envLocal env_local.Env,
	blobErrors collections_slice.Slice[BlobError],
) {
	ui.Err().Printf("blobs with errors: %d", blobErrors.Len())

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
