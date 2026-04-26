package inventory_log

import (
	"sync"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Registry maps event type-string to Codec. Native types are reserved;
// importers register only new types. Registration of a reserved type
// panics at both Global and per-Observer scope.
type Registry interface {
	Register(c Codec)
	Lookup(typeStr string) (Codec, bool)
}

// Global is the package-level registry. Importers MAY register codecs
// from init(); FileObserver consults Global at Emit time after its own
// per-Observer overrides.
var Global Registry = newRegistry()

// reservedTypes is the set of type-strings owned by inventory_log.
// Importers cannot register against any of these.
var reservedTypes = map[string]struct{}{
	"blob-write-published-v1": {},
}

type registry struct {
	mu     sync.RWMutex
	codecs map[string]Codec
}

func newRegistry() *registry {
	return &registry{codecs: make(map[string]Codec)}
}

func (r *registry) Register(c Codec) {
	typeStr := c.Type()

	if _, reserved := reservedTypes[typeStr]; reserved {
		panic(errors.ErrorWithStackf(
			"codec type %q is reserved by inventory_log; importers cannot register against it",
			typeStr,
		).Error())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.codecs[typeStr]; exists {
		panic(errors.ErrorWithStackf(
			"codec type %q is already registered",
			typeStr,
		).Error())
	}

	r.codecs[typeStr] = c
}

func (r *registry) Lookup(typeStr string) (Codec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.codecs[typeStr]
	return c, ok
}

// registerNative installs a codec for a reserved type. Bypasses the
// reserved-type panic since these are the codecs the reservation
// protects. Called only from inventory_log's own init().
func registerNative(c Codec) {
	typeStr := c.Type()

	if _, reserved := reservedTypes[typeStr]; !reserved {
		panic(errors.ErrorWithStackf(
			"registerNative called with non-reserved type %q",
			typeStr,
		).Error())
	}

	r := Global.(*registry)
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.codecs[typeStr]; exists {
		panic(errors.ErrorWithStackf(
			"native codec type %q is already registered",
			typeStr,
		).Error())
	}

	r.codecs[typeStr] = c
}
