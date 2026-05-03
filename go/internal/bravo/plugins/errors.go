package plugins

import "errors"

var (
	// ErrAlreadyRegistered is returned by Registry.Register when the
	// reference is already registered.
	ErrAlreadyRegistered = errors.New("plugin already registered")

	// ErrUnknownPlugin is returned by Registry.Resolve when the
	// reference is not registered.
	ErrUnknownPlugin = errors.New("unknown plugin reference")
)
