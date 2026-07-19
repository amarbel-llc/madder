package blob_transfers

import (
	"time"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

func MakeBlobImporter(
	envRepo command_components.BlobStoreEnv,
	src blob_stores.BlobStoreInitialized,
	dsts blob_stores.BlobStoreMap,
) BlobImporter {
	return BlobImporter{
		EnvBlobStore: envRepo,
		Src:          src,
		Dsts:         dsts,
	}
}

type BlobImporter struct {
	EnvBlobStore           command_components.BlobStoreEnv
	CopierDelegate         interfaces.FuncIter[blob_stores.CopyResult]
	Src                    blob_stores.BlobStoreInitialized
	Dsts                   blob_stores.BlobStoreMap
	UseDestinationHashType bool

	Counts Counts
}

type Counts struct {
	Succeeded int
	Ignored   int
	Failed    int
	Total     int
}

func (blobImporter *BlobImporter) ImportBlobIfNecessary(
	blobId domain_interfaces.MarklId,
) (err error) {
	if len(blobImporter.Dsts) == 0 {
		return blobImporter.emitMissingBlob(blobId)
	}

	for _, blobStore := range blobImporter.Dsts {
		copyResult := blobImporter.ImportBlobToStoreIfNecessary(
			blobStore,
			blobId,
		)

		if err = copyResult.GetError(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}

func (blobImporter *BlobImporter) emitMissingBlob(
	blobId domain_interfaces.MarklId,
) (err error) {
	copyResult := blob_stores.CopyResult{
		BlobId: blobId,
	}

	copyResult.SetBlobMissingLocally()

	if blobImporter.Src.HasBlob(blobId) {
		copyResult.SetBlobExistsLocally()
	}

	if err = blobImporter.emitCopyResultIfNecessary(copyResult); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

func (blobImporter *BlobImporter) emitCopyResultIfNecessary(
	copyResult blob_stores.CopyResult,
) (err error) {
	if blobImporter.CopierDelegate == nil {
		return err
	}

	if err = blobImporter.CopierDelegate(copyResult); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

func (blobImporter *BlobImporter) ImportBlobToStoreIfNecessary(
	dst blob_stores.BlobStoreInitialized,
	blobId domain_interfaces.MarklId,
) (copyResult blob_stores.CopyResult) {
	var progressWriter env_ui.ProgressWriter

	if err := errors.RunChildContextWithPrintTicker(
		blobImporter.EnvBlobStore,
		func(ctx errors.Context) {
			blobImporter.Counts.Total++

			var hashType domain_interfaces.FormatHash

			if blobImporter.UseDestinationHashType {
				hashType = dst.GetDefaultHashType()
			}

			copyResult = blob_stores.CopyBlobIfNecessary(
				blobImporter.EnvBlobStore,
				dst.GetBlobStore(),
				blobImporter.Src.GetBlobStore(),
				blobId,
				&progressWriter,
				hashType,
			)

			if copyResult.IsError() {
				blobImporter.Counts.Failed++
				ctx.Cancel(copyResult.GetError())
			} else if copyResult.IsMissing() {
				blobImporter.Counts.Failed++
			} else if copyResult.Exists() {
				blobImporter.Counts.Ignored++
			} else {
				blobImporter.Counts.Succeeded++
			}

			if err := blobImporter.emitCopyResultIfNecessary(
				copyResult,
			); err != nil {
				copyResult.SetError(errors.Wrap(err))
				return
			}
		},
		func(time time.Time) {
			blobImporter.printCopyProgress(blobId, &progressWriter)
		},
		3*time.Second,
	); err != nil {
		copyResult.SetError(errors.Wrap(err))
		return copyResult
	}

	return copyResult
}

// printCopyProgress emits the periodic copy-progress line through the
// importer env's err sink (honoring env_ui's CustomErr /
// UIFileIsStderr) rather than the process-global ui.Err() stderr
// printer, so consumers can redirect transfer chatter per-env — the
// same contract #228 established for blob-store construction chatter.
// See #229.
func (blobImporter *BlobImporter) printCopyProgress(
	blobId domain_interfaces.MarklId,
	progressWriter *env_ui.ProgressWriter,
) {
	blobImporter.EnvBlobStore.GetErr().Printf(
		"Copying %s... (%s written)",
		blobId,
		progressWriter.GetWrittenHumanString(),
	)
}
