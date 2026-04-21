package futility

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/protocol"
)

type (
	// Cmd is the interface for dodder-style commands that use the Request pattern.
	// Commands implement Run(Request) and optionally implement
	// CommandWithDescription, CommandWithParams, CommandWithResult, etc.
	Cmd interface {
		Run(Request)
	}

	// CommandWithDescription is implemented by Cmd types that provide metadata.
	CommandWithDescription interface {
		GetDescription() Description
	}

	// CommandWithParams is the opt-in interface for declaring parameters.
	// Commands returning both flags and positional args (via Positional: true
	// on the Param) get automatic MCP schema generation and CLI dispatch.
	CommandWithParams interface {
		GetParams() []Param
	}

	// CommandWithResult is implemented by Cmd types that can return a
	// structured Result for MCP tool responses. Commands implementing
	// this interface get registered as MCP tools via Utility.AddCmd.
	// Commands implementing only Cmd (not CommandWithResult) are CLI-only.
	CommandWithResult interface {
		RunResult(Request) (*Result, error)
	}

	// CommandWithExamples supplies per-command usage examples.
	CommandWithExamples interface {
		GetExamples() []Example
	}

	// CommandWithEnvVars supplies per-command environment variables.
	CommandWithEnvVars interface {
		GetEnvVars() []EnvVar
	}

	// CommandWithFiles supplies per-command filesystem paths.
	CommandWithFiles interface {
		GetFiles() []FilePath
	}

	// CommandWithSeeAlso supplies per-command SEE ALSO references.
	CommandWithSeeAlso interface {
		GetSeeAlso() []string
	}

	// CommandWithAnnotations supplies MCP tool annotations directly as the
	// richer protocol type (replaces the old MCPAnnotations struct).
	CommandWithAnnotations interface {
		GetAnnotations() *protocol.ToolAnnotations
	}
)
