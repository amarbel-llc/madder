package commands_hyphence

import (
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("validate", &Validate{})
}

type Validate struct{}

var _ futility.CommandWithParams = (*Validate)(nil)

func (cmd *Validate) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Validate) GetDescription() futility.Description {
	return futility.Description{
		Short: "strict RFC 0001 conformance check",
		Long: "Read a hyphence document and verify it conforms to RFC " +
			"0001. Exits 0 silent on pass; exits 65 with one line- " +
			"numbered diagnostic on stderr on the first violation. " +
			"Validate also enforces the inline-body-AND-@ rule (RFC " +
			"0001 §Metadata Lines): a document MUST NOT carry both an " +
			"@ blob-reference line and a body section.",
	}
}

func (cmd *Validate) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Validate) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		bail(req, "validate", path, err)
		return
	}
	defer closer.Close()

	if err := validateDocument(in); err != nil {
		bail(req, "validate", source, err)
		return
	}
}

// validateDocument runs the strict-RFC validation pipeline against in
// and returns the first violation, or nil on success. Shared between
// Validate.Run (which prints diagnostics and cancels the request) and
// the test seam in validate_test.go.
func validateDocument(in io.Reader) error {
	v := &hyphence.MetadataValidator{}
	body := &hyphence.CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        v,
		Blob:            body,
	}
	if _, err := reader.ReadFrom(in); err != nil {
		return err
	}
	if v.SawAtLine && body.SawBody {
		return hyphence.ErrInlineBodyWithAtReference
	}
	return nil
}
