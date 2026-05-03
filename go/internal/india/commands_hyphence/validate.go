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
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	v := &hyphence.MetadataValidator{}
	body := &CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        v,
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: validate: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}

	if v.SawAtLine && body.SawBody {
		fmt.Fprintf(os.Stderr, "hyphence: validate: %s: %s\n", source, hyphence.ErrInlineBodyWithAtReference)
		errors.ContextCancelWithBadRequestError(req, hyphence.ErrInlineBodyWithAtReference)
		return
	}
}

// CountingDiscardReaderFrom is the Blob consumer for validate, meta,
// and any subcommand that wants to drain the body section without
// preserving it. SawBody is true after ReadFrom if at least one byte
// followed the body separator.
type CountingDiscardReaderFrom struct {
	SawBody bool
}

func (c *CountingDiscardReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(io.Discard, r)
	if n > 0 {
		c.SawBody = true
	}
	return n, err
}
