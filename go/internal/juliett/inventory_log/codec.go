package inventory_log

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Codec serializes one LogEvent shape to and from one NDJSON line.
// Constructed via MakeCodec[E]; the registry holds the type-erased form.
// One codec per type-string. Native types are reserved; see Registry.
type Codec interface {
	Type() string

	// Encode marshals event to a single JSON object including a top-level
	// "type" field. Trailing newline is added by the Observer; Encode
	// returns just the JSON bytes.
	Encode(event domain_interfaces.LogEvent) ([]byte, error)

	// Decode unmarshals one NDJSON line into the typed payload.
	// Used by readers and tests.
	Decode(line []byte) (domain_interfaces.LogEvent, error)
}

// MakeCodec binds a concrete event type E to a type-string and returns
// the type-erased Codec the registry stores. Type assertion happens once
// inside Encode; callers see typed encode/decode signatures.
func MakeCodec[E domain_interfaces.LogEvent](
	typeStr string,
	encode func(E) ([]byte, error),
	decode func([]byte) (E, error),
) Codec {
	return codec[E]{typ: typeStr, enc: encode, dec: decode}
}

type codec[E domain_interfaces.LogEvent] struct {
	typ string
	enc func(E) ([]byte, error)
	dec func([]byte) (E, error)
}

func (c codec[E]) Type() string { return c.typ }

func (c codec[E]) Encode(event domain_interfaces.LogEvent) ([]byte, error) {
	typed, ok := event.(E)
	if !ok {
		var zero E
		return nil, errors.ErrorWithStackf(
			"codec %q: expected event type %T, got %T",
			c.typ, zero, event,
		)
	}
	return c.enc(typed)
}

func (c codec[E]) Decode(line []byte) (domain_interfaces.LogEvent, error) {
	return c.dec(line)
}
