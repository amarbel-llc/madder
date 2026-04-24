package commands

import (
	"io"
	"slices"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/foxtrot/config_cli"
)

func init() {
	utility.AddCmd(
		"complete",
		&Complete{})
}

type Complete struct {
	bashStyle  bool
	inProgress string
}

var (
	_ interfaces.CommandComponentWriter = (*Complete)(nil)
	_ futility.CommandWithParams        = (*Complete)(nil)
)

func (cmd *Complete) GetParams() []futility.Param { return nil }

func (cmd Complete) GetDescription() futility.Description {
	return futility.Description{
		Short: "complete a command-line",
	}
}

func (cmd *Complete) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	flagDefinitions.BoolVar(&cmd.bashStyle, "bash-style", false, "")
	flagDefinitions.StringVar(&cmd.inProgress, "in-progress", "", "")
}

func (cmd Complete) makeEnv(req futility.Request) env_local.Env {
	config := command_components.DefaultConfig

	var debugOptions debug.Options
	var cliConfig domain_interfaces.CLIConfigProvider

	if config != nil {
		debugOptions = config.Debug
		cliConfig = config
	}

	dir := env_dir.MakeDefault(
		req,
		req.Utility.GetName(),
		debugOptions,
	)

	return env_local.Make(
		env_ui.Make(
			req,
			cliConfig,
			debugOptions,
			env_ui.Options{},
		),
		dir,
	)
}

func (cmd Complete) Run(req futility.Request) {
	utility := req.Utility
	envLocal := cmd.makeEnv(req)

	// TODO extract into constructor
	// TODO find double-hyphen
	// TODO keep track of all args
	commandLine := futility.CommandLineInput{
		FlagsOrArgs: req.PeekArgs(),
		InProgress:  cmd.inProgress,
	}

	// TODO determine state:
	// bare: `madder`
	// subcommand or arg or flag:
	//  - `madder subcommand`
	//  - `madder subcommand -flag=true`
	//  - `madder subcommand -flag value`
	// flag: `madder subcommand -flag`
	lastArg, hasLastArg := commandLine.LastArg()

	if !hasLastArg {
		cmd.completeSubcommands(envLocal, commandLine, utility)
		return
	}

	name := req.PopArg("name")
	subcmd, foundSubcmd := utility.GetCommand(name)

	if !foundSubcmd {
		cmd.completeSubcommands(envLocal, commandLine, utility)
		return
	}

	flagSet := flags.NewFlagSet(name, flags.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	(&config_cli.Config{}).SetFlagDefinitions(flagSet)

	// TODO: migrate flag completion to use subcmd.Params instead of
	// SetFlagDefinitions interface (subcmd is *Command, not an interface)

	var containsDoubleHyphen bool

	if slices.Contains(commandLine.FlagsOrArgs, "--") {
		containsDoubleHyphen = true
	}

	if !containsDoubleHyphen &&
		cmd.completeSubcommandFlags(
			req,
			envLocal,
			subcmd,
			flagSet,
			commandLine,
			lastArg,
		) {
		return
	}

	cmd.completeSubcommandArgs(req, envLocal, subcmd, commandLine)
}

func (cmd Complete) completeSubcommands(
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
	utility *futility.Utility,
) {
	for name, subcmd := range utility.AllCommands() {
		cmd.completeSubcommand(envLocal, name, subcmd)
	}
}

func (cmd Complete) completeSubcommand(
	envLocal env_local.Env,
	name string,
	subcmd *futility.Command,
) {
	shortDescription := subcmd.Description.Short

	if shortDescription != "" {
		envLocal.GetUI().Printf("%s\t%s", name, shortDescription)
	} else {
		envLocal.GetUI().Printf("%s", name)
	}
}

func (cmd Complete) completeSubcommandArgs(
	req futility.Request,
	envLocal env_local.Env,
	subcmd *futility.Command,
	commandLine futility.CommandLineInput,
) {
	if subcmd == nil {
		return
	}

	// TODO: migrate arg completion to use subcmd.Params enum values
	// instead of Completer interface (subcmd is *Command, not an interface)
	_ = req
	_ = envLocal
	_ = commandLine
}

func (cmd Complete) completeSubcommandFlags(
	req futility.Request,
	envLocal env_local.Env,
	subcmd *futility.Command,
	flagSet *flags.FlagSet, commandLine futility.CommandLineInput,
	lastArg string,
) (shouldNotCompleteArgs bool) {
	if subcmd == nil {
		return shouldNotCompleteArgs
	}

	if strings.HasPrefix(lastArg, "-") && commandLine.InProgress != "" {
		shouldNotCompleteArgs = true
	} else if commandLine.InProgress != "" && len(commandLine.FlagsOrArgs) > 1 {
		lastArg = commandLine.FlagsOrArgs[len(commandLine.FlagsOrArgs)-2]
		commandLine.InProgress = ""
		shouldNotCompleteArgs = strings.HasPrefix(lastArg, "-")
	}

	if commandLine.InProgress != "" {
		flagSet.VisitAll(func(flag *flags.Flag) {
			envLocal.GetUI().Printf("-%s\t%s", flag.Name, flag.Usage)
		})
	} else if err := flagSet.Parse([]string{lastArg}); err != nil {
		cmd.completeSubcommandFlagOnParseError(
			req,
			envLocal,
			subcmd,
			flagSet,
			commandLine,
			err,
		)
	} else {
		flagSet.VisitAll(func(flag *flags.Flag) {
			envLocal.GetUI().Printf("-%s\t%s", flag.Name, flag.Usage)
		})
	}

	return shouldNotCompleteArgs
}

func (cmd Complete) completeSubcommandFlagOnParseError(
	req futility.Request,
	envLocal env_local.Env,
	subcmd *futility.Command,
	flagSet *flags.FlagSet,
	commandLine futility.CommandLineInput,
	err error,
) {
	if subcmd == nil {
		return
	}

	after, found := strings.CutPrefix(
		err.Error(),
		"flag needs an argument: -",
	)

	if !found {
		errors.ContextCancelWithBadRequestError(envLocal, err)
		return
	}

	var flag *flags.Flag

	if flag = flagSet.Lookup(after); flag == nil {
		// exception
		errors.ContextCancelWithErrorf(
			envLocal,
			"expected to find flag %q, but none found. All flags: %#v",
			after,
			flagSet,
		)

		return
	}

	flagValue := flag.Value

	switch flagValue := flagValue.(type) {
	case interface{ GetCLICompletion() map[string]string }:
		completions := flagValue.GetCLICompletion()

		for name, description := range completions {
			if name != "" && description != "" {
				envLocal.GetUI().Printf("%s\t%s", name, description)
			} else if description == "" {
				envLocal.GetUI().Printf("%s", name)
			} else {
				envLocal.GetErr().Printf("empty flag value for %s (description: %q)", flag.Name, description)
			}
		}

	case futility.Completer:
		flagValue.Complete(req, envLocal, commandLine)

	default:
		errors.ContextCancelWithBadRequestf(
			req,
			"no completion available for flag: %q. Flag Value: %T, *flag.Flag %#v",
			after,
			flagValue,
			flag,
		)
	}
}
