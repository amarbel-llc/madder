package commands_hyphence

import (
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("meta", &Meta{})
}

type Meta struct{}

var _ futility.CommandWithParams = (*Meta)(nil)

func (cmd *Meta) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Meta) GetDescription() futility.Description {
	return futility.Description{
		Short: "print metadata section verbatim",
		Long: "Read a hyphence document and print the metadata section " +
			"to stdout, with the surrounding `---` boundaries " +
			"stripped. No per-line validation runs — malformed prefixes " +
			"are printed through. Boundary-level errors (missing closing " +
			"`---`, missing body separator) still abort. Run `hyphence " +
			"validate` first if strict checks matter.",
	}
}

func (cmd *Meta) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Meta) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		bail(req, "meta", path, err)
		return
	}
	defer closer.Close()

	streamer := &hyphence.MetadataStreamer{W: os.Stdout}
	body := &hyphence.CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        streamer,
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		bail(req, "meta", source, err)
		return
	}
}
