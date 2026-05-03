package commands_hyphence

import (
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("format", &Format{})
}

type Format struct{}

var _ futility.CommandWithParams = (*Format)(nil)

func (cmd *Format) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Format) GetDescription() futility.Description {
	return futility.Description{
		Short: "re-emit canonicalized per RFC §Canonical Line Order",
		Long: "Read a hyphence document and re-emit it with metadata " +
			"lines sorted per RFC 0001 §Canonical Line Order: " +
			"description (#) → object references (<) → tags (-) → blob " +
			"reference (@) → type (!). Within each prefix, source " +
			"order is preserved. Comments (%) stay anchored to their " +
			"following non-comment line. Body bytes pass through " +
			"unchanged.",
	}
}

func (cmd *Format) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Format) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		bail(req, "format", path, err)
		return
	}
	defer closer.Close()

	doc := &hyphence.Document{}
	builder := &hyphence.MetadataBuilder{Doc: doc}
	emitter := &hyphence.FormatBodyEmitter{Doc: doc, Out: os.Stdout}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        builder,
		Blob:            emitter,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		bail(req, "format", source, err)
		return
	}
}
