//go:build test

package blob_io

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"

	deweyerrors "code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

func TestIsBlobStoreUnavailable_Nil(t *testing.T) {
	if IsBlobStoreUnavailable(nil) {
		t.Fatalf("nil error should not be unavailable")
	}
}

func TestIsBlobStoreUnavailable_WrappedSentinel(t *testing.T) {
	root := errors.New("ssh: connection refused")
	wrapped := MakeErrBlobStoreUnavailable("archive", "ssh dial", root)
	if !IsBlobStoreUnavailable(wrapped) {
		t.Fatalf("wrapped ErrBlobStoreUnavailable not detected")
	}

	// Wrap once more via dewey errors.Wrapf to confirm errors.As
	// still resolves through the chain.
	doubleWrapped := deweyerrors.Wrapf(wrapped, "outer frame")
	if !IsBlobStoreUnavailable(doubleWrapped) {
		t.Fatalf("dewey-wrapped ErrBlobStoreUnavailable not detected")
	}

	// errors.Is(target, ErrBlobStoreUnavailable{}) should also work
	// for callers that prefer Is over the predicate.
	if !errors.Is(doubleWrapped, ErrBlobStoreUnavailable{}) {
		t.Fatalf("errors.Is sentinel match failed")
	}

	// Unwrap must reach the original cause.
	if got := errors.Unwrap(wrapped); !errors.Is(got, root) {
		t.Fatalf("Unwrap did not return original cause: got %v want %v", got, root)
	}
}

func TestMakeErrBlobStoreUnavailable_NilCauseReturnsNil(t *testing.T) {
	if got := MakeErrBlobStoreUnavailable("x", "y", nil); got != nil {
		t.Fatalf("nil cause should produce nil error, got %v", got)
	}
}

func TestErrBlobStoreUnavailable_ErrorString(t *testing.T) {
	err := ErrBlobStoreUnavailable{
		StoreId: "archive",
		Reason:  "ssh dial",
		Cause:   errors.New("connection refused"),
	}
	msg := err.Error()
	for _, want := range []string{
		"blob store unavailable",
		"store=archive",
		"ssh dial",
		"connection refused",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() missing %q: %s", want, msg)
		}
	}
}

func TestIsBlobStoreUnavailable_NetOpError(t *testing.T) {
	opErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ECONNREFUSED,
	}
	if !IsBlobStoreUnavailable(opErr) {
		t.Fatalf("*net.OpError not detected as unavailable")
	}

	// Through a dewey wrap.
	wrapped := deweyerrors.Wrapf(opErr, "ssh dial")
	if !IsBlobStoreUnavailable(wrapped) {
		t.Fatalf("dewey-wrapped *net.OpError not detected")
	}
}

func TestIsBlobStoreUnavailable_DNSError(t *testing.T) {
	dnsErr := &net.DNSError{
		Err:        "no such host",
		Name:       "host.invalid",
		IsNotFound: true,
	}
	if !IsBlobStoreUnavailable(dnsErr) {
		t.Fatalf("*net.DNSError not detected as unavailable")
	}
}

// timeoutErr lets us synthesize a net.Error whose Timeout() returns
// true without depending on a real socket-deadline expiry.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "synthetic timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestIsBlobStoreUnavailable_NetTimeout(t *testing.T) {
	if !IsBlobStoreUnavailable(timeoutErr{}) {
		t.Fatalf("net.Error.Timeout()==true not detected")
	}
}

// nonTimeoutNetErr exercises the case where errors.As matches
// net.Error but Timeout() is false; we should NOT classify as
// unavailable on that signal alone (avoiding masking unrelated
// errors that happen to also satisfy net.Error).
type nonTimeoutNetErr struct{}

func (nonTimeoutNetErr) Error() string   { return "synthetic non-timeout" }
func (nonTimeoutNetErr) Timeout() bool   { return false }
func (nonTimeoutNetErr) Temporary() bool { return false }

func TestIsBlobStoreUnavailable_NetErrorNonTimeoutNotMatched(t *testing.T) {
	if IsBlobStoreUnavailable(nonTimeoutNetErr{}) {
		t.Fatalf("non-timeout net.Error should not classify as unavailable")
	}
}

func TestIsBlobStoreUnavailable_SSHHandshakeString(t *testing.T) {
	err := errors.New("ssh: handshake failed: ssh: unable to authenticate")
	if !IsBlobStoreUnavailable(err) {
		t.Fatalf("ssh handshake string not detected")
	}
}

func TestIsBlobStoreUnavailable_SSHAuthString(t *testing.T) {
	err := errors.New(
		"ssh: unable to authenticate, attempted methods [none publickey], " +
			"no supported methods remain",
	)
	if !IsBlobStoreUnavailable(err) {
		t.Fatalf("ssh auth string not detected")
	}
}

func TestIsBlobStoreUnavailable_KnownHostsMissing(t *testing.T) {
	err := errors.New(
		"no known_hosts files found; create ~/.ssh/known_hosts, set $SSH_HOME, " +
			"or specify --known-hosts-file",
	)
	if !IsBlobStoreUnavailable(err) {
		t.Fatalf("known_hosts-missing string not detected")
	}
}

func TestIsBlobStoreUnavailable_SSHAgent(t *testing.T) {
	for _, msg := range []string{
		"SSH_AUTH_SOCK empty or unset",
		"failed to connect to SSH_AUTH_SOCK: dial unix /tmp/ssh: connect: no such file",
	} {
		if !IsBlobStoreUnavailable(errors.New(msg)) {
			t.Fatalf("ssh-agent string %q not detected", msg)
		}
	}
}

func TestIsBlobStoreUnavailable_BlobMissingNotUnavailable(t *testing.T) {
	// ErrBlobMissing is a well-formed "this store doesn't have it"
	// response, distinct from unavailability. The classifier MUST
	// NOT promote it to unavailable: it would mask genuine
	// not-found cases as transport failures.
	missing := ErrBlobMissing{
		Path: "/some/path",
	}
	if IsBlobStoreUnavailable(missing) {
		t.Fatalf("ErrBlobMissing must not classify as unavailable")
	}
}

func TestIsBlobStoreUnavailable_GenericErrorNotMatched(t *testing.T) {
	// A vanilla error should not be unavailable. Guards against
	// accidental matches via overly-broad substring rules.
	if IsBlobStoreUnavailable(errors.New("blob corrupted at offset 17")) {
		t.Fatalf("unrelated error misclassified as unavailable")
	}
}

func TestIsBlobStoreUnavailable_DeadlineExceededViaNetTimeout(t *testing.T) {
	// i/o timeout errors surface as net.Error.Timeout()==true; the
	// classifier should catch them even when the concrete type is a
	// wrapped *net.OpError carrying a timeout inner error.
	netErr := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: timeoutErr{},
	}
	if !IsBlobStoreUnavailable(netErr) {
		t.Fatalf("read-deadline net.OpError not detected")
	}
}
