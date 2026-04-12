package sku

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

type Edges struct {
	Objects []ids.ObjectId
	Blobs   []markl.Id
	Skipped []error
}

type EdgeExplorer interface {
	ExploreEdges(object *Transacted) (Edges, error)
}
