package blob_io

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrBlobStoreUnavailable wraps a transport-level error (network
// unreachable, SSH dial / handshake / auth failure, etc.) that means
// the backend store could not be consulted at all — distinct from a
// well-formed "blob not found" response. Multi-store fallback paths
// (Multi.MakeBlobReader, per-command blobFromRemainingStores) treat
// this as miss-equivalent and continue the walk.
//
// Backends that can distinguish unavailability from other errors at
// the call site SHOULD wrap their unavailability returns in this type
// so the classifier resolves the case via errors.As without having to
// pattern-match opaque error strings. The classifier
// IsBlobStoreUnavailable also recognises bare *net.OpError, DNS
// errors, and net.Error.Timeout() returns so adapters that haven't
// adopted the wrapper still benefit; future backends should prefer
// the wrapper for precision.
//
// Closes #209.
type ErrBlobStoreUnavailable struct {
	// StoreId is the human-readable identity of the backend that
	// reported the unavailability, used in diagnostics ("sftp store
	// archive unavailable: ..."). Empty when the backend doesn't
	// have a stable id at the wrap site.
	StoreId string

	// Reason is a short stable phrase describing what went wrong
	// (e.g. "ssh dial", "ssh handshake", "ssh auth"). Optional.
	Reason string

	// Cause is the underlying error. Always non-nil for wrapped
	// instances; errors.Unwrap returns it so callers can drill
	// further if they want the raw transport error.
	Cause error
}

func (err ErrBlobStoreUnavailable) Error() string {
	parts := []string{"blob store unavailable"}
	if err.StoreId != "" {
		parts = append(parts, fmt.Sprintf("store=%s", err.StoreId))
	}
	if err.Reason != "" {
		parts = append(parts, err.Reason)
	}
	if err.Cause != nil {
		parts = append(parts, err.Cause.Error())
	}
	return strings.Join(parts, ": ")
}

func (err ErrBlobStoreUnavailable) Unwrap() error {
	return err.Cause
}

// Is matches any other ErrBlobStoreUnavailable so callers can use
// errors.Is(err, blob_io.ErrBlobStoreUnavailable{}) as a sentinel
// match against unwrap chains that don't go through errors.As.
func (err ErrBlobStoreUnavailable) Is(target error) bool {
	_, ok := target.(ErrBlobStoreUnavailable)
	return ok
}

func (err ErrBlobStoreUnavailable) GetErrorType() pkgErrDisamb {
	return pkgErrDisamb{}
}

// IsBlobStoreUnavailable reports whether err represents a
// transport-level failure that means a blob-store backend could not
// be consulted — as opposed to a well-formed "blob not found" or
// I/O-corruption response. Multi-store fallback paths treat true as
// a miss-equivalent and continue probing the remaining stores.
//
// Matching strategy, in priority order:
//
//  1. errors.As against ErrBlobStoreUnavailable. Backends that wrap
//     their dial/handshake/auth boundary in the typed error get exact
//     classification.
//  2. errors.As against *net.OpError (TCP dial refused, no route to
//     host, connection reset).
//  3. errors.As against *net.DNSError (host unresolvable, NXDOMAIN).
//  4. errors.As against net.Error with Timeout() == true (read/write
//     deadline exceeded, dial timeout).
//  5. Substring match against known SSH-handshake / auth-failure
//     strings from golang.org/x/crypto/ssh. The ssh package does not
//     export typed errors for these conditions today; the substring
//     match is the documented fallback. New SFTP error returns
//     should prefer wrapping in ErrBlobStoreUnavailable at the dial
//     boundary so future cases land in case (1) instead.
//
// A nil error is not unavailable.
func IsBlobStoreUnavailable(err error) bool {
	if err == nil {
		return false
	}

	var unavailable ErrBlobStoreUnavailable
	if errors.As(err, &unavailable) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// SSH handshake / auth failures from golang.org/x/crypto/ssh
	// do not have public typed forms. The two stable signatures we
	// see in practice are "ssh: handshake failed" and "ssh: unable
	// to authenticate". Keep this list narrow — we'd rather miss a
	// novel SSH failure mode and have the caller see the real error
	// than mask an unrelated bug as "unavailable".
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ssh: handshake failed"):
		return true
	case strings.Contains(msg, "ssh: unable to authenticate"):
		return true
	case strings.Contains(msg, "SSH_AUTH_SOCK empty or unset"):
		return true
	case strings.Contains(msg, "failed to connect to SSH_AUTH_SOCK"):
		return true
	case strings.Contains(msg, "no known_hosts files found"):
		return true
	}

	return false
}

// MakeErrBlobStoreUnavailable wraps cause in an
// ErrBlobStoreUnavailable. Returns nil when cause is nil so callers
// can use it unconditionally on a wrap path that may not have an
// error.
func MakeErrBlobStoreUnavailable(
	storeId string,
	reason string,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	return ErrBlobStoreUnavailable{
		StoreId: storeId,
		Reason:  reason,
		Cause:   cause,
	}
}
