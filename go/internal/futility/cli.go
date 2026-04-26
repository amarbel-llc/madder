package futility

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/collections_slice"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

// cmdComponentKey maps a *Command back to the original Cmd that registered
// it through AddCmd, so RunCLI can call SetFlagDefinitions on the Cmd's own
// struct pointers before parsing. Only populated for AddCmd registrations.
//
// We store this on the Utility so that CLI dispatch can invoke the Cmd's
// interfaces.CommandComponentWriter hook against the live FlagSet.
var commandComponentWriters = map[*Command]interfaces.CommandComponentWriter{}

// registerComponentWriter is called from AddCmd to remember the Cmd's
// SetFlagDefinitions binding for later FlagSet registration in RunCLI.
func registerComponentWriter(cmd *Command, ccw interfaces.CommandComponentWriter) {
	commandComponentWriters[cmd] = ccw
}

// RunCLI parses CLI arguments, dispatches to the matched command, and prints
// the result. Global params and the bare "help"/-h/--help words print usage.
// Prefix subcommands joined by hyphens are resolved from space-separated args
// (e.g. "perms check" → "perms-check").
func (u *Utility) RunCLI(ctx context.Context, args []string, p Prompter) error {
	if len(args) == 0 {
		u.printUsage()
		return nil
	}

	// The bare `help` / `-h` / `--help` words short-circuit to utility
	// usage before any flag parsing so `tool -h` works even when `-h` is
	// not a registered global flag (which it usually isn't, since the
	// flags package handles --help internally and would reject it from a
	// custom FlagSet).
	if isHelpArg(args[0]) {
		u.printUsage()
		return nil
	}

	// Parse any global flags that precede the subcommand name. flags.Parse
	// stops at the first non-flag arg (the subcommand), so walking args
	// through globalFS.Parse drains leading --foo tokens and leaves
	// remaining pointing at the subcommand onwards. The GlobalFlagDefiner
	// is the sole binding path — GlobalParams is declarative metadata for
	// rendering only, never a registration path, because the two would
	// redefine the same flag name on the FlagSet otherwise.
	globalFS := flags.NewFlagSet(u.Name, flags.ContinueOnError)
	if u.GlobalFlagDefiner != nil {
		u.GlobalFlagDefiner(globalFS)
	}
	if err := globalFS.Parse(args); err != nil {
		return fmt.Errorf("parsing global flags: %w", err)
	}
	remaining := globalFS.Args()

	// Snapshot the globals the user explicitly set before the subcommand.
	// Rebinding GlobalFlagDefiner on the per-command FlagSet below rewrites
	// the struct fields back to their defaults (BoolVar et al. init the
	// pointer on each bind), so we restore these after cmd parse for any
	// flag not also re-specified post-subcommand.
	globalsSetPreSubcommand := make(map[string]string)
	globalFS.Visit(func(f *flags.Flag) {
		globalsSetPreSubcommand[f.Name] = f.Value.String()
	})

	if len(remaining) == 0 {
		u.printUsage()
		return nil
	}

	name := remaining[0]
	cmdArgs := remaining[1:]

	cmd, ok := u.GetCommand(name)
	if ok {
		// Resolve deeper prefix subcommands: "mcp stdio" → "mcp-stdio"
		for len(cmdArgs) > 0 && !strings.HasPrefix(cmdArgs[0], "-") {
			deeper := name + "-" + cmdArgs[0]
			if deeperCmd, found := u.GetCommand(deeper); found {
				name = deeper
				cmd = deeperCmd
				cmdArgs = cmdArgs[1:]
			} else {
				break
			}
		}
	} else {
		// Try joining with subsequent args for prefix subcommands:
		// "perms check" → "perms-check"
		for i := 1; i < len(remaining); i++ {
			name = name + "-" + remaining[i]
			if cmd, ok = u.GetCommand(name); ok {
				cmdArgs = remaining[i+1:]
				break
			}
		}
		if !ok {
			return fmt.Errorf("unknown command: %s", remaining[0])
		}
	}

	if hasHelpFlag(cmdArgs) {
		u.printCommandUsage(name, cmd)
		return nil
	}

	if cmd.Run == nil {
		// Commands with subcommands but no handler show usage.
		prefix := name + "-"
		for n := range u.commands {
			if strings.HasPrefix(n, prefix) {
				u.printCommandUsage(name, cmd)
				return nil
			}
		}
		return fmt.Errorf("command %s has no handler", name)
	}

	// Build a FlagSet that includes any Param flags and any SetFlagDefinitions
	// struct-pointer flags, parse against it, then walk Params in declaration
	// order to populate the Request's CommandLineInput.Args.
	fs := flags.NewFlagSet(name, flags.ContinueOnError)

	// Register Param-declared flags (non-positional).
	registerParamFlags(fs, cmd.Params)

	// Let the Cmd's SetFlagDefinitions register its struct-pointer flags.
	if ccw, has := commandComponentWriters[cmd]; has {
		ccw.SetFlagDefinitions(fs)
	}

	// Call the GlobalFlagDefiner against the per-command FlagSet so its
	// struct-pointer bindings land on the same storage as the pre-subcommand
	// parse. This is what makes `tool sub --no-inventory-log` and
	// `tool --no-inventory-log sub` equivalent at the value level.
	if u.GlobalFlagDefiner != nil {
		u.GlobalFlagDefiner(fs)
	}

	if err := fs.Parse(cmdArgs); err != nil {
		return fmt.Errorf("parsing flags for %s: %w", name, err)
	}

	// Restore any global flag values the user set pre-subcommand that
	// were reset by the GlobalFlagDefiner rebind above and not re-set by
	// cmdArgs post-subcommand. This preserves `tool --verbose sub` while
	// leaving `tool sub --verbose` and `tool sub` (no flag) untouched.
	cmdFSVisited := make(map[string]bool)
	fs.Visit(func(f *flags.Flag) {
		cmdFSVisited[f.Name] = true
	})
	for globalName, val := range globalsSetPreSubcommand {
		if cmdFSVisited[globalName] {
			continue
		}
		if f := fs.Lookup(globalName); f != nil {
			_ = f.Value.Set(val)
		}
	}

	positional := fs.Args()

	// Build CommandLineInput.Args from params in declaration order:
	// for flags, read the parsed value from the FlagSet;
	// for positional args, consume from the remaining `positional` slice.
	var cliArgs collections_slice.String
	pi := 0
	for _, param := range cmd.Params {
		pn := param.paramName()

		if !param.isPositional() {
			if f := fs.Lookup(pn); f != nil {
				cliArgs.Append(f.Value.String())
			}
			continue
		}

		if pi >= len(positional) {
			continue
		}
		if param.isVariadic() {
			cliArgs.Append(positional[pi:]...)
			pi = len(positional)
			break
		}
		cliArgs.Append(positional[pi])
		pi++
	}

	errCtx := errors.MakeContextDefault()
	input := CommandLineInput{Args: cliArgs}
	req := Request{
		Context:  errCtx,
		Utility:  u,
		Prompter: p,
		FlagSet:  fs,
		input:    &input,
	}

	result, err := cmd.Run(req)
	if err != nil {
		return err
	}
	printResult(result)
	return nil
}

// registerParamFlags creates FlagSet entries for every non-positional Param.
// Each flag gets both the long name and, when non-zero, its single-character
// short alias. Values are stored in temporary heap-allocated variables; the
// caller reads them back via fs.Lookup after Parse.
func registerParamFlags(fs *flags.FlagSet, params []Param) {
	for _, param := range params {
		if param.isPositional() {
			continue
		}
		pn := param.paramName()
		short := param.paramShort()
		desc := param.paramDescription()

		switch param.jsonSchemaType() {
		case "boolean":
			b := new(bool)
			defVal := false
			if d, ok := param.paramDefault().(bool); ok {
				defVal = d
			}
			fs.BoolVar(b, pn, defVal, desc)
			if short != 0 {
				fs.BoolVar(b, string(short), defVal, desc)
			}
		case "integer":
			i := new(int)
			defVal := 0
			if d, ok := param.paramDefault().(int); ok {
				defVal = d
			}
			fs.IntVar(i, pn, defVal, desc)
			if short != 0 {
				fs.IntVar(i, string(short), defVal, desc)
			}
		default:
			s := new(string)
			defVal := ""
			if d, ok := param.paramDefault().(string); ok {
				defVal = d
			}
			fs.StringVar(s, pn, defVal, desc)
			if short != 0 {
				fs.StringVar(s, string(short), defVal, desc)
			}
		}
	}
}

func printResult(r *Result) {
	if r == nil {
		return
	}
	if r.JSON != nil {
		data, _ := json.MarshalIndent(r.JSON, "", "  ")
		fmt.Println(string(data))
	} else if r.Text != "" {
		fmt.Println(r.Text)
	}
}

func isHelpArg(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func (u *Utility) printCommandUsage(name string, cmd *Command) {
	displayName := strings.ReplaceAll(name, "-", " ")
	fmt.Printf("%s %s — %s\n\n", u.Name, displayName, cmd.Description.Short)
	if cmd.Description.Long != "" {
		fmt.Printf("%s\n\n", cmd.Description.Long)
	}
	if len(cmd.Params) > 0 {
		fmt.Println("Options:")
		for _, p := range cmd.Params {
			if p.isPositional() {
				continue
			}
			flag := fmt.Sprintf("--%s", p.paramName())
			if p.paramShort() != 0 {
				flag = fmt.Sprintf("-%c, --%s", p.paramShort(), p.paramName())
			}
			fmt.Printf("  %-24s %s\n", flag, p.paramDescription())
		}
	}

	// List subcommands (commands starting with name-)
	prefix := name + "-"
	var subs []sortedCommand
	for n, c := range u.VisibleCommands() {
		if strings.HasPrefix(n, prefix) {
			subs = append(subs, sortedCommand{strings.TrimPrefix(n, prefix), c})
		}
	}
	if len(subs) > 0 {
		sort.Slice(subs, func(i, j int) bool {
			return subs[i].name < subs[j].name
		})
		if len(cmd.Params) > 0 {
			fmt.Println()
		}
		fmt.Println("Subcommands:")
		for _, s := range subs {
			fmt.Printf("  %-16s %s\n", s.name, s.cmd.Description.Short)
		}
	}
}

func (u *Utility) printUsage() {
	fmt.Printf("%s — %s\n\n", u.Name, u.Description.Short)
	if u.Description.Long != "" {
		fmt.Printf("%s\n\n", u.Description.Long)
	}

	cmds := u.sortedVisibleCommands()

	// Identify group prefixes: commands whose name is a prefix of other commands.
	groups := make(map[string]bool)
	for _, e := range cmds {
		for _, other := range cmds {
			if strings.HasPrefix(other.name, e.name+"-") {
				groups[e.name] = true
				break
			}
		}
	}

	fmt.Println("Commands:")
	for _, e := range cmds {
		// Hide children of group commands from top-level listing.
		isChild := false
		for g := range groups {
			if strings.HasPrefix(e.name, g+"-") {
				isChild = true
				break
			}
		}
		if isChild {
			continue
		}
		fmt.Printf("  %-16s %s\n", e.name, e.cmd.Description.Short)
	}
}
