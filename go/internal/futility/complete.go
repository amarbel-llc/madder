package futility

import (
	"fmt"
	"os"
)

// addCompleteCommand registers the hidden __complete subcommand that provides
// dynamic value completions at tab-completion time. Shell completion scripts
// call this to get completions for params that have a Completer function.
//
// Usage: appname __complete --command <subcmd> --param <paramname>
// Output: tab-separated "value\tdescription" lines, one per completion candidate.
func (u *Utility) addCompleteCommand() {
	u.AddCmd("__complete", &completeCmd{util: u})

	// __complete is hidden from listings and manpages.
	if cmd, ok := u.GetCommand("__complete"); ok {
		cmd.Hidden = true
	}
}

type completeCmd struct {
	util *Utility
}

func (c *completeCmd) GetDescription() Description {
	return Description{
		Short: "Internal tab-completion helper (hidden).",
	}
}

func (c *completeCmd) GetParams() []Param {
	return []Param{
		StringFlag{Name: "command", Required: true, Description: "Subcommand name"},
		StringFlag{Name: "param", Required: true, Description: "Parameter name"},
	}
}

func (c *completeCmd) Run(req Request) {
	commandName := req.PopArg("command")
	paramName := req.PopArg("param")

	cmd, ok := c.util.GetCommand(commandName)
	if !ok {
		return // unknown command, no completions
	}

	for _, p := range cmd.Params {
		if p.paramName() != paramName {
			continue
		}
		if f, ok := p.(interface{ flagCompleter() ParamCompleter }); ok {
			if completer := f.flagCompleter(); completer != nil {
				printCompletions(completer)
			}
		}
		return
	}
}

// printCompletions writes completion candidates from an iterator to stdout.
func printCompletions(completions ParamCompleter) {
	for c := range completions {
		if c.Description != "" {
			fmt.Fprintf(os.Stdout, "%s\t%s\n", c.Value, c.Description)
		} else {
			fmt.Fprintln(os.Stdout, c.Value)
		}
	}
}
