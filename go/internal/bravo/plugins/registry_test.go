package plugins

import (
	"errors"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

func TestRegistry_RegisterAndResolve(t *testing.T) {
	r := newRegistry()
	stub := stubFactory(func() interfaces.IOWrapper { return nil })
	if err := r.Register("test-codec-v1@stub", stub); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Resolve("test-codec-v1@stub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != nil {
		t.Errorf("stub factory should produce nil; got %T", got)
	}
}

func TestRegistry_DuplicateRegisterFails(t *testing.T) {
	r := newRegistry()
	stub := stubFactory(func() interfaces.IOWrapper { return nil })
	if err := r.Register("test-codec-v1@dup", stub); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register("test-codec-v1@dup", stub)
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("expected ErrAlreadyRegistered, got %v", err)
	}
}

func TestRegistry_UnknownReferenceFails(t *testing.T) {
	r := newRegistry()
	_, err := r.Resolve("test-codec-v1@missing")
	if !errors.Is(err, ErrUnknownPlugin) {
		t.Errorf("expected ErrUnknownPlugin, got %v", err)
	}
	if !strings.Contains(err.Error(), "test-codec-v1@missing") {
		t.Errorf("error should mention the bad reference: %v", err)
	}
}

type stubFactory func() interfaces.IOWrapper

func (s stubFactory) New() interfaces.IOWrapper { return s() }
