package commands_hyphence

import (
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("body", &Body{})
}

type Body struct{}

var _ futility.CommandWithParams = (*Body)(nil)

func (cmd *Body) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Body) GetDescription() futility.Description {
	return futility.Description{
		Short: "print body section verbatim",
		Long: "Read a hyphence document and stream its body section " +
			"(the bytes after the closing --- and the body separator) " +
			"to stdout. If the document has no body, prints nothing and " +
			"exits 0.",
	}
}

func (cmd *Body) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Body) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	body := &writerReaderFrom{W: os.Stdout}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        &CountingDiscardReaderFrom{},
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: body: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
}

// writerReaderFrom is the Blob consumer for the body subcommand:
// stream bytes from r straight to W.
type writerReaderFrom struct{ W io.Writer }

func (w *writerReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.W, r)
}
