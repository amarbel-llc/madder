package futility

import (
	"context"
	"encoding/json"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/protocol"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/server"
)

// RegisterMCPTools registers all non-hidden commands as MCP tools
// in the given ToolRegistry, using each command's description and
// auto-generated JSON schema.
func (u *Utility) RegisterMCPTools(registry *server.ToolRegistry) {
	for name, cmd := range u.AllCommands() {
		if cmd.Hidden || cmd.Run == nil {
			continue
		}

		c := cmd // capture for closure
		registry.Register(
			name,
			cmd.Description.Short,
			cmd.InputSchema(),
			func(ctx context.Context, args json.RawMessage) (*protocol.ToolCallResult, error) {
				result, err := dispatchMCP(u, c, args)
				if err != nil {
					return nil, err
				}
				return resultToMCP(result), nil
			},
		)
	}
}

// RegisterMCPToolsV1 registers all non-hidden commands as V1 MCP tools
// in the given ToolRegistryV1.
func (u *Utility) RegisterMCPToolsV1(registry *server.ToolRegistryV1) {
	for name, cmd := range u.AllCommands() {
		if cmd.Hidden || cmd.Run == nil {
			continue
		}

		c := cmd // capture for closure
		registry.Register(
			protocol.ToolV1{
				Name:        name,
				Title:       cmd.Title,
				Description: cmd.Description.Short,
				InputSchema: cmd.InputSchema(),
				Annotations: cmd.Annotations,
				Execution:   cmd.Execution,
			},
			func(ctx context.Context, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
				result, err := dispatchMCP(u, c, args)
				if err != nil {
					return nil, err
				}
				return resultToMCPV1(result), nil
			},
		)
	}
}

// dispatchMCP converts the raw MCP JSON args into a CommandLineInput using
// the command's declared Params, constructs a Request with a fresh
// errors.Context, and invokes Command.Run.
func dispatchMCP(u *Utility, cmd *Command, args json.RawMessage) (*Result, error) {
	errCtx := errors.MakeContextDefault()
	input := makeInputFromJSON(args, cmd.Params)

	// If the wrapped Cmd uses SetFlagDefinitions to bind struct-pointer flags,
	// walk JSON values through a FlagSet so the struct pointers are populated.
	fs := flags.NewFlagSet(cmd.Name, flags.ContinueOnError)
	if ccw, has := commandComponentWriters[cmd]; has {
		ccw.SetFlagDefinitions(fs)
		applyJSONToFlagSet(fs, args)
	}

	req := Request{
		Context:  errCtx,
		Utility:  u,
		Prompter: StubPrompter{},
		FlagSet:  fs,
		input:    &input,
	}

	// Command.Run handlers installed by AddCmd already wrap the user's Cmd in
	// errCtx.Run so that PopArg/Cancel surface as normal errors. Commands
	// registered directly via AddCommand with a hand-written Run are
	// responsible for their own error handling.
	return cmd.Run(req)
}

// applyJSONToFlagSet sets each recognised flag in fs from the matching JSON
// value in args. Unknown keys are ignored. Non-string JSON values are passed
// as their raw representation (booleans and integers decode correctly via
// FlagSet.Set).
func applyJSONToFlagSet(fs *flags.FlagSet, args json.RawMessage) {
	if len(args) == 0 {
		return
	}

	var vals map[string]json.RawMessage
	if err := json.Unmarshal(args, &vals); err != nil {
		return
	}

	for flagName, rawVal := range vals {
		if fs.Lookup(flagName) == nil {
			continue
		}
		var s string
		if err := json.Unmarshal(rawVal, &s); err != nil {
			s = string(rawVal)
		}
		_ = fs.Set(flagName, s)
	}
}

func resultToMCPV1(r *Result) *protocol.ToolCallResultV1 {
	if r == nil {
		return &protocol.ToolCallResultV1{}
	}
	var text string
	if r.JSON != nil {
		data, _ := json.Marshal(r.JSON)
		text = string(data)
	} else {
		text = r.Text
	}
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1(text)},
		IsError: r.IsErr,
	}
}

func resultToMCP(r *Result) *protocol.ToolCallResult {
	if r == nil {
		return &protocol.ToolCallResult{}
	}
	var text string
	if r.JSON != nil {
		data, _ := json.Marshal(r.JSON)
		text = string(data)
	} else {
		text = r.Text
	}
	return &protocol.ToolCallResult{
		Content: []protocol.ContentBlock{protocol.TextContent(text)},
		IsError: r.IsErr,
	}
}
