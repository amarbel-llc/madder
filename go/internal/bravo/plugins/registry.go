package plugins

import (
	"sync"

	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

// registry is the in-process plugin index. The package-level Default
// registry is populated at init() by each plugin subpackage.
type registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func newRegistry() *registry {
	return &registry{factories: map[string]Factory{}}
}

func (r *registry) Register(reference string, f Factory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[reference]; ok {
		return errors.Errorf("%w: %s", ErrAlreadyRegistered, reference)
	}
	r.factories[reference] = f
	return nil
}

func (r *registry) Resolve(reference string) (interfaces.IOWrapper, error) {
	r.mu.RLock()
	f, ok := r.factories[reference]
	r.mu.RUnlock()
	if !ok {
		return nil, errors.Errorf("%w: %s", ErrUnknownPlugin, reference)
	}
	return f.New(), nil
}

// Default is the package-level registry, populated by plugin
// subpackages at init time.
var Default = newRegistry()

// MustRegister registers a plugin in the Default registry; panics on
// failure. Used from plugin subpackage init() functions where a
// duplicate registration is a programming error.
func MustRegister(reference string, f Factory) {
	if err := Default.Register(reference, f); err != nil {
		panic(err)
	}
}

// Resolve looks up reference in the Default registry. Returns
// (nil, error wrapping ErrUnknownPlugin) when absent.
func Resolve(reference string) (interfaces.IOWrapper, error) {
	return Default.Resolve(reference)
}
