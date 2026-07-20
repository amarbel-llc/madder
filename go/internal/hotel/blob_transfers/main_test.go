//go:build test

package blob_transfers

import (
	"bytes"
	"strings"
	"testing"

	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/builtins"
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_store_env"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestPrintCopyProgress_RoutesToEnvErrSink pins the blob_transfers half
// of #229: the periodic "Copying <id>... (<n> written)" ticker line
// must route through the importer env's err sink (which honors
// env_ui's CustomErr / UIFileIsStderr) rather than the process-global
// ui.Err() stderr printer — the same contract #228 established for
// blob-store construction chatter.
func TestPrintCopyProgress_RoutesToEnvErrSink(t *testing.T) {
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

	blobImporter := BlobImporter{
		EnvBlobStore: blob_store_env.BlobStoreEnv{Env: envLocal},
	}

	blobId, repool := markl.FormatHashSha256.GetMarklIdForString("progress")
	t.Cleanup(repool)

	var progressWriter env_ui.ProgressWriter

	blobImporter.printCopyProgress(blobId, &progressWriter)

	out := buf.String()
	if !strings.Contains(out, "Copying") {
		t.Fatalf(
			"copy-progress chatter did not reach the env err sink; buffer: %q",
			out,
		)
	}
}
