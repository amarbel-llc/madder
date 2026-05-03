package commands_hyphence

import (
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// NoInputError wraps a file-open failure that should map to the
// CLI's EX_NOINPUT (66) exit code at the top level. The wrapped err
// preserves the os.PathError/os.IsNotExist semantics for callers
// that need to inspect them.
type NoInputError struct {
	Path string
	Err  error
}

func (e *NoInputError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Err)
}
func (e *NoInputError) Unwrap() error { return e.Err }

// OpenInput resolves the positional <path|-> argument for every
// hyphence subcommand. When path is "-", the supplied stdin reader
// is used and source is reported as "-". When path is anything else,
// the file is opened; failure to open is wrapped in *NoInputError so
// the CLI maps it to EX_NOINPUT.
//
// The returned closer is always non-nil; callers should defer
// closer.Close().
func OpenInput(path string, stdin io.Reader) (io.Reader, string, io.Closer, error) {
	if path == "-" {
		if stdin == nil {
			return nil, "", io.NopCloser(nil), errors.ErrorWithStackf("stdin is nil")
		}
		return stdin, "-", io.NopCloser(stdin), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, path, io.NopCloser(nil), &NoInputError{Path: path, Err: err}
	}
	return f, path, f, nil
}

// bail prints a gcc-style diagnostic to stderr and cancels the request.
// Centralizes the "hyphence: <subcommand>: <source>: <err>" format
// every subcommand uses on its error path.
func bail(req futility.Request, subcommand, source string, err error) {
	fmt.Fprintf(os.Stderr, "hyphence: %s: %s: %s\n", subcommand, source, err)
	errors.ContextCancelWithBadRequestError(req, err)
}
