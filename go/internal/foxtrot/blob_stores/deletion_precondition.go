package blob_stores

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// DeletionPrecondition checks whether blobs are safe to delete from the
// loose store. The default implementation always returns nil (safe).
// Future implementations can verify off-host replication before allowing
// deletion.
type DeletionPrecondition interface {
	CheckBlobsSafeToDelete(
		blobs interfaces.SeqError[domain_interfaces.MarklId],
	) error
}

type nopDeletionPrecondition struct{}

func (nopDeletionPrecondition) CheckBlobsSafeToDelete(
	blobs interfaces.SeqError[domain_interfaces.MarklId],
) error {
	return nil
}

func NopDeletionPrecondition() DeletionPrecondition {
	return nopDeletionPrecondition{}
}
