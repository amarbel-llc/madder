package inventory_log

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// AsBlobWriteObserver wraps an Observer so it satisfies the existing
// domain_interfaces.BlobWriteObserver contract. Stores keep calling
// OnBlobPublished; the adapter forwards to Observer.Emit.
func AsBlobWriteObserver(o Observer) domain_interfaces.BlobWriteObserver {
	return blobWriteAdapter{o: o}
}

type blobWriteAdapter struct {
	o Observer
}

var _ domain_interfaces.BlobWriteObserver = blobWriteAdapter{}

func (a blobWriteAdapter) OnBlobPublished(ev domain_interfaces.BlobWriteEvent) {
	a.o.Emit(ev)
}
