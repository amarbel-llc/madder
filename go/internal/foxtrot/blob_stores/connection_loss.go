package blob_stores

import (
	stderrors "errors"
	"net"
	"strings"

	"github.com/pkg/sftp"

	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
)

// IsConnectionLost reports whether err says a request failed because the
// connection to a remote store dropped mid-flight. Doing the same request
// again on a fresh connection is expected to work, so a caller that can
// safely repeat it (the copy of one content-addressed blob can) may try once
// more. A store that has given up reconnecting is a different matter; see
// IsStoreGivenUp.
func IsConnectionLost(err error) bool {
	return err != nil &&
		(stderrors.Is(err, sftp.ErrSSHFxConnectionLost) ||
			stderrors.Is(err, sftp.ErrSSHFxNoConnection) ||
			stderrors.Is(err, net.ErrClosed))
}

// IsStoreGivenUp reports whether err says a store could not be reached and
// will not be tried again: it was unreachable at the start, or its
// connection dropped and every reconnect attempt failed. Going on to the
// next blob would only fail the same way, so a bulk operation should stop.
func IsStoreGivenUp(err error) bool {
	var unavailable blob_io.ErrBlobStoreUnavailable

	return err != nil && stderrors.As(err, &unavailable)
}

// ErrorText is err as one line a person can act on. A joined error's own
// text is only "error group: N errors", which is what a sync record carried
// when a connection dropped (madder#306); this spells out the errors inside.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0)

		for _, inner := range joined.Unwrap() {
			if text := ErrorText(inner); text != "" {
				parts = append(parts, text)
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}

	return err.Error()
}
