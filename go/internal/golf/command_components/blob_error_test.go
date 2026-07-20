//go:build test

package command_components

import (
	"bytes"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/collections_slice"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/debug"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// TestPrintBlobErrors_HeaderRoutesToEnvErrSink pins the
// command_components half of #229: the "blobs with errors: N" header
// must land on the same env err sink as the per-blob error lines that
// follow it (which already went through envLocal.GetErr()), not on the
// process-global ui.Err() stderr printer.
func TestPrintBlobErrors_HeaderRoutesToEnvErrSink(t *testing.T) {
	var buf bytes.Buffer

	envLocal := env_local.Make(
		env_ui.Make(
			errors.MakeContextDefault(),
			nil,
			debug.Options{},
			env_ui.Options{CustomErr: &buf},
		),
		nil,
	)

	var blobErrors collections_slice.Slice[BlobError]
	blobErrors.Append(BlobError{
		Err: errors.Errorf("synthetic blob failure"),
	})

	PrintBlobErrors(envLocal, blobErrors)

	out := buf.String()
	if !strings.Contains(out, "blobs with errors: 1") {
		t.Fatalf(
			"header did not reach the env err sink; buffer: %q",
			out,
		)
	}
	if !strings.Contains(out, "synthetic blob failure") {
		t.Errorf(
			"per-blob error line missing from the env err sink; buffer: %q",
			out,
		)
	}
}
