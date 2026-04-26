package inventory_log

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// TestGlobalRegister_ReservedTypePanics asserts the reserved-types
// policy at Global.Register: importers cannot shadow native codecs.
func TestGlobalRegister_ReservedTypePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for reserved type, got none")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "reserved") {
			t.Errorf("panic message missing 'reserved': %q", msg)
		}
	}()

	bogus := MakeCodec[domain_interfaces.BlobWriteEvent](
		"blob-write-published-v1",
		func(domain_interfaces.BlobWriteEvent) ([]byte, error) { return nil, nil },
		func([]byte) (domain_interfaces.BlobWriteEvent, error) {
			return domain_interfaces.BlobWriteEvent{}, nil
		},
	)
	Global.Register(bogus)
}

// TestObserverRegister_ReservedTypePanics asserts the same policy at
// the per-Observer scope: per-instance shadowing of a native codec
// also panics. (Symmetric with Global.Register.)
func TestObserverRegister_ReservedTypePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for reserved type, got none")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "reserved") {
			t.Errorf("panic message missing 'reserved': %q", msg)
		}
	}()

	bogus := MakeCodec[domain_interfaces.BlobWriteEvent](
		"blob-write-published-v1",
		func(domain_interfaces.BlobWriteEvent) ([]byte, error) { return nil, nil },
		func([]byte) (domain_interfaces.BlobWriteEvent, error) {
			return domain_interfaces.BlobWriteEvent{}, nil
		},
	)

	o := NewFileObserver(t.TempDir())
	o.RegisterCodec(bogus)
}

// TestGlobalRegister_DuplicateNonReservedPanics keeps the
// importer-side contract simple: two codecs for the same non-reserved
// type-string is a programmer error, not a "last write wins" race.
type duplicateEvent struct{}

func (duplicateEvent) LogType() string { return "inventory_log_test-dup-v1" }

func TestGlobalRegister_DuplicateNonReservedPanics(t *testing.T) {
	mk := func() Codec {
		return MakeCodec[duplicateEvent](
			"inventory_log_test-dup-v1",
			func(duplicateEvent) ([]byte, error) { return nil, nil },
			func([]byte) (duplicateEvent, error) { return duplicateEvent{}, nil },
		)
	}

	Global.Register(mk())

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "already registered") {
			t.Errorf("panic message missing 'already registered': %q", msg)
		}
	}()

	Global.Register(mk())
}
