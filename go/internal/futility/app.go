package futility

import (
	"fmt"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Utility holds the command registry and top-level metadata for a CLI/MCP application.
type Utility struct {
	Name        string
	Aliases     []string // Aliases are additional binary names that should get shell completions
	Description Description
	Examples    []Example // app-level workflow examples

	// EnvVars are environment variables the app as a whole reads, rendered
	// into the app manpage's ENVIRONMENT section.
	EnvVars []EnvVar

	// Files are filesystem paths the app as a whole reads or writes, rendered
	// into the app manpage's FILES section.
	Files []FilePath

	// ExtraManpages are hand-written manpage source files (any roff dialect)
	// to install alongside the auto-generated pages. Each entry is read from
	// its Source fs.FS and written verbatim to share/man/man{Section}/{Name}.
	// The framework does not parse, validate, or modify these files —
	// authors choose any dialect (man(7), mdoc(7), or pre-rendered output
	// from scdoc/ronn/asciidoctor).
	ExtraManpages []ManpageFile

	commands       map[string]*Command
	canonicalNames map[*Command]string
}

// NewUtility creates a new Utility with the given name and short description.
func NewUtility(name, short string) *Utility {
	u := &Utility{
		Name:           name,
		Description:    Description{Short: short},
		commands:       make(map[string]*Command),
		canonicalNames: make(map[*Command]string),
	}

	u.addCompleteCommand()

	return u
}

// AddCommand registers a command and its aliases. Panics on duplicate names
// or if any two command params share the same non-zero Short rune.
func (u *Utility) AddCommand(cmd *Command) {
	// TODO: reintroduce global-flag vs command-flag short-flag collision
	// detection if/when global Params return.

	// Check for duplicate short flags within the command's own params.
	shortSeen := make(map[rune]string)
	for _, cp := range cmd.Params {
		short := cp.paramShort()
		if short == 0 {
			continue
		}
		if existing, ok := shortSeen[short]; ok {
			panic(fmt.Sprintf(
				"duplicate short flag -%c: used by both %q and %q",
				short, existing, cp.paramName(),
			))
		}
		shortSeen[short] = cp.paramName()
	}

	u.addName(cmd.Name, cmd)
	for _, alias := range cmd.Aliases {
		u.addName(alias, cmd)
	}
}

func (u *Utility) addName(name string, cmd *Command) {
	if _, ok := u.commands[name]; ok {
		panic(fmt.Sprintf("command added more than once: %s", name))
	}
	u.commands[name] = cmd
	if _, ok := u.canonicalNames[cmd]; !ok {
		u.canonicalNames[cmd] = name
	}
}

// GetName returns the utility's name.
func (u *Utility) GetName() string {
	return u.Name
}

// GetCommand looks up a command by name or alias.
func (u *Utility) GetCommand(name string) (*Command, bool) {
	cmd, ok := u.commands[name]
	return cmd, ok
}

// AllCommands iterates over all registered commands (including hidden).
// Each unique command is yielded once even if it has aliases.
func (u *Utility) AllCommands() func(yield func(string, *Command) bool) {
	return func(yield func(string, *Command) bool) {
		seen := make(map[*Command]bool)
		for _, cmd := range u.commands {
			if seen[cmd] {
				continue
			}
			seen[cmd] = true
			if !yield(u.canonicalNames[cmd], cmd) {
				return
			}
		}
	}
}

// VisibleCommands iterates over non-hidden commands.
func (u *Utility) VisibleCommands() func(yield func(string, *Command) bool) {
	return func(yield func(string, *Command) bool) {
		for name, cmd := range u.AllCommands() {
			if cmd.Hidden {
				continue
			}
			if !yield(name, cmd) {
				return
			}
		}
	}
}

// AddCmd wraps a dodder-style Cmd into a *Command and registers it.
// Metadata is extracted from opt-in interfaces:
//   - CommandWithDescription → Command.Description
//   - CommandWithParams → Command.Params
//   - CommandWithExamples → Command.Examples
//   - CommandWithEnvVars → Command.EnvVars
//   - CommandWithFiles → Command.Files
//   - CommandWithSeeAlso → Command.SeeAlso
//   - CommandWithAnnotations → Command.Annotations
//   - CommandWithResult → Run returns a *Result from RunResult
//
// If the wrapped Cmd also implements interfaces.CommandComponentWriter,
// its SetFlagDefinitions is invoked before dispatch so struct-field-bound
// flags are wired in.
func (u *Utility) AddCmd(name string, cmd Cmd) {
	wrapped := &Command{
		Name: name,
	}

	if cwd, ok := cmd.(CommandWithDescription); ok {
		wrapped.Description = cwd.GetDescription()
	}

	if cwp, ok := cmd.(CommandWithParams); ok {
		wrapped.Params = cwp.GetParams()
	}

	if cwe, ok := cmd.(CommandWithExamples); ok {
		wrapped.Examples = cwe.GetExamples()
	}

	if cwe, ok := cmd.(CommandWithEnvVars); ok {
		wrapped.EnvVars = cwe.GetEnvVars()
	}

	if cwf, ok := cmd.(CommandWithFiles); ok {
		wrapped.Files = cwf.GetFiles()
	}

	if cws, ok := cmd.(CommandWithSeeAlso); ok {
		wrapped.SeeAlso = cws.GetSeeAlso()
	}

	if cwa, ok := cmd.(CommandWithAnnotations); ok {
		wrapped.Annotations = cwa.GetAnnotations()
	}

	if ccw, ok := cmd.(interfaces.CommandComponentWriter); ok {
		registerComponentWriter(wrapped, ccw)
	}

	// Runtime dispatch: the framework is responsible for constructing the
	// Request (including the errors.Context and any CommandLineInput). Both
	// CLI and MCP paths call Command.Run(req) directly.
	wrapped.Run = func(req Request) (*Result, error) {
		errCtx := req.Context

		if cwr, ok := cmd.(CommandWithResult); ok {
			var result *Result
			var resultErr error
			err := errCtx.Run(func(_ errors.Context) {
				result, resultErr = cwr.RunResult(req)
			})
			if err != nil {
				return nil, err
			}
			return result, resultErr
		}

		err := errCtx.Run(func(_ errors.Context) {
			cmd.Run(req)
		})
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	u.AddCommand(wrapped)
}

// MergeWithPrefix adds all commands from another Utility, prefixed with the given string.
func (u *Utility) MergeWithPrefix(other *Utility, prefix string) {
	for name, cmd := range other.AllCommands() {
		key := name
		if prefix != "" {
			key = prefix + "-" + name
		}
		u.addName(key, cmd)
		u.canonicalNames[cmd] = key
	}
}
