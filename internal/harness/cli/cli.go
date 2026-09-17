// Package cli projects a compiled YAML CLI contract onto Cobra. It performs no
// product routing or runtime construction: those remain the dispatcher's work.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Invocation struct {
	ConfigFile  string
	Command     []string
	Arguments   []string
	Flags       map[string]any
	Dispatch    spec.CLIDispatch
	Mode        string
	FrontendRef string
}
type IO struct {
	In       io.Reader
	Out, Err io.Writer
	Help     func() error
}
type Options struct {
	Version, Commit string
	IsTerminal      bool
	LookupEnv       func(string) (string, bool)
}

// Error preserves the policy class and authored process exit code.
type Error struct {
	Class  string
	Code   int
	Err    error
	Prefix string
}

func (e *Error) Error() string { return e.Prefix + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }
func (e *Error) ExitCode() int { return e.Code }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

// New builds a fresh command tree with no global flags, handlers or templates.
// SetIn/SetOut/SetErr on the returned root configure every projection's IO.
func New(document *spec.Document, dispatch func(context.Context, Invocation, IO) error, options Options) (*cobra.Command, error) {
	if document == nil || document.Spec.Interfaces.CLI == nil {
		return nil, fmt.Errorf("compiled document must declare spec.interfaces.cli")
	}
	contract := document.Spec.Interfaces.CLI
	if err := spec.ValidateCLI(document); err != nil {
		return nil, &Error{Class: "configuration", Code: contract.ExitCodes.Configuration, Err: err, Prefix: contract.Presentation.Errors.Prefix}
	}
	if dispatch == nil {
		return nil, &Error{Class: "configuration", Code: contract.ExitCodes.Configuration, Err: fmt.Errorf("CLI dispatcher is required"), Prefix: contract.Presentation.Errors.Prefix}
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	b := builder{document: document, contract: contract, dispatch: dispatch, options: options, lookupEnv: lookupEnv}
	var err error
	if b.helpTemplate, err = parseTemplate("help", contract.Presentation.Help.Template); err != nil {
		return nil, b.error("configuration", fmt.Errorf("invalid YAML help template: %w", err))
	}
	if b.usageTemplate, err = parseTemplate("usage", contract.Presentation.Help.UsageTemplate); err != nil {
		return nil, b.error("configuration", fmt.Errorf("invalid YAML usage template: %w", err))
	}
	help := contract.Presentation.Help
	helpFlag := spec.CLIFlag{Name: help.Flag, Shorthand: help.Shorthand, Type: "bool", Default: "false", Usage: help.FlagUsage, Scope: "inherited"}
	root, err := b.command(contract.Root, nil, map[string]spec.CLIFlag{help.Flag: helpFlag})
	if err != nil {
		return nil, err
	}
	root.PersistentFlags().BoolP(help.Flag, help.Shorthand, false, help.FlagUsage)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(os.Stdin)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	for _, command := range root.Commands() {
		if b.routes[command].Operation == "help" {
			root.SetHelpCommand(command)
		}
	}
	var verify func(*cobra.Command) error
	verify = func(command *cobra.Command) error {
		if err := b.renderHelpTo(command, io.Discard); err != nil {
			return b.error("configuration", fmt.Errorf("invalid YAML help presentation: %w", err))
		}
		for _, child := range command.Commands() {
			if err := verify(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := verify(root); err != nil {
		return nil, err
	}
	return root, nil
}

type builder struct {
	document      *spec.Document
	contract      *spec.CLI
	dispatch      func(context.Context, Invocation, IO) error
	options       Options
	lookupEnv     func(string) (string, bool)
	helpTemplate  *template.Template
	usageTemplate *template.Template
	routes        map[*cobra.Command]spec.CLIDispatch
}

func (b *builder) command(def spec.CLICommand, prefix []string, inherited map[string]spec.CLIFlag) (*cobra.Command, error) {
	path := append(append([]string(nil), prefix...), def.Name)
	use := def.Name
	if def.Usage != "" {
		use = def.Usage
	}
	cmd := &cobra.Command{
		Use: use, Short: def.Short, Long: def.Long, Example: def.Example,
		Aliases: append([]string(nil), def.Aliases...), Deprecated: def.Deprecated, Hidden: def.Hidden,
		ValidArgs:     append([]string(nil), def.Args.Enum...),
		SilenceErrors: true, SilenceUsage: true,
		// Cobra must preserve argument order and reject flags it cannot represent.
		DisableFlagsInUseLine: true,
	}
	version := b.options.Version
	if version == "" {
		version = b.contract.Identity.Version
	}
	cmd.Annotations = map[string]string{"version": version, "commit": b.options.Commit, "compatibility": b.contract.Identity.Compatibility}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpFunc(func(command *cobra.Command, args []string) {
		if err := b.renderHelpTo(command, command.OutOrStdout()); err != nil {
			fmt.Fprintln(command.ErrOrStderr(), b.contract.Presentation.Errors.Prefix+"unable to render help")
		}
	})
	cmd.SetUsageFunc(func(command *cobra.Command) error {
		view := helpView(command)
		return b.usageTemplate.Execute(command.OutOrStdout(), view)
	})
	cmd.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		// The underlying parser may echo a secret value supplied as a flag.
		// Retain the actionable class without reproducing the value.
		return b.error("usage", errors.New("invalid command flags"))
	})
	active := make(map[string]spec.CLIFlag, len(inherited)+len(def.Flags))
	childrenInherited := make(map[string]spec.CLIFlag, len(inherited)+len(def.Flags))
	for name, flag := range inherited {
		active[name], childrenInherited[name] = flag, flag
	}
	for _, flag := range def.Flags {
		active[flag.Name] = flag
		if flag.Scope == "inherited" {
			childrenInherited[flag.Name] = flag
		}
		if err := addFlag(cmd, flag); err != nil {
			return nil, b.error("configuration", fmt.Errorf("flag %s has an invalid definition", flag.Name))
		}
	}
	cmd.Args = func(command *cobra.Command, args []string) error {
		if len(args) < def.Args.Min || (def.Args.Max != -1 && len(args) > def.Args.Max) {
			return b.error("usage", fmt.Errorf("%s expects %s", command.CommandPath(), argumentRange(def.Args)))
		}
		for _, arg := range args {
			if len(def.Args.Enum) > 0 && !contains(def.Args.Enum, arg) {
				return b.error("usage", fmt.Errorf("%s argument must be one of %s", command.CommandPath(), strings.Join(def.Args.Enum, ", ")))
			}
		}
		return nil
	}
	cmd.RunE = func(command *cobra.Command, args []string) error {
		values, err := b.flagValues(command, active)
		if err != nil {
			return err
		}
		if def.Dispatch == nil {
			return b.renderHelp(command)
		}
		route := *def.Dispatch
		invocation := Invocation{
			ConfigFile: b.document.Source,
			Command:    append([]string(nil), path...), Arguments: append([]string(nil), args...),
			Flags: values, Dispatch: route, FrontendRef: route.FrontendRef,
		}
		switch route.Operation {
		case "help":
			target := command.Root()
			if len(args) > 0 {
				var remaining []string
				target, remaining, err = command.Root().Find(args)
				if err != nil || len(remaining) > 0 {
					return b.error("usage", errors.New("unknown help command"))
				}
			}
			return b.renderHelp(target)
		case "completion":
			return b.completion(command.Root(), args[0], command.OutOrStdout())
		case "unsupported":
			return b.error("unsupported", errors.New("command is unsupported by this CLI contract"))
		case "input":
			invocation.Mode = b.inputMode(route.Input, args)
			if invocation.Mode == "repl" && route.Input.TerminalFrontendRef != "" {
				invocation.FrontendRef = route.Input.TerminalFrontendRef
			}
			if invocation.Mode == "" || (invocation.Mode == "args" && len(args) == 0) {
				if route.Input.Empty == "help" {
					return b.renderHelp(command)
				}
				return b.error("usage", errors.New("input is required"))
			}
		}
		streams := IO{In: command.InOrStdin(), Out: command.OutOrStdout(), Err: command.ErrOrStderr(), Help: func() error { return b.renderHelp(command) }}
		if err := b.dispatch(command.Context(), invocation, streams); err != nil {
			var typed *Error
			if errors.As(err, &typed) {
				return &Error{Class: typed.Class, Code: typed.Code, Err: typed.Err, Prefix: b.contract.Presentation.Errors.Prefix}
			}
			class := "runtime"
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				class = "interrupted"
			}
			return b.error(class, err)
		}
		return nil
	}
	if b.routes == nil {
		b.routes = make(map[*cobra.Command]spec.CLIDispatch)
	}
	if def.Dispatch != nil {
		b.routes[cmd] = *def.Dispatch
	}
	for _, child := range def.Commands {
		childCommand, err := b.command(child, path, childrenInherited)
		if err != nil {
			return nil, err
		}
		cmd.AddCommand(childCommand)
	}
	return cmd, nil
}

func addFlag(command *cobra.Command, flag spec.CLIFlag) error {
	set := command.Flags()
	if flag.Scope == "inherited" {
		set = command.PersistentFlags()
	}
	value, err := spec.ParseCLIFlagValue(flag.Type, flag.Default)
	if err != nil {
		return err
	}
	switch flag.Type {
	case "string":
		set.StringP(flag.Name, flag.Shorthand, value.(string), flag.Usage)
	case "bool":
		set.BoolP(flag.Name, flag.Shorthand, value.(bool), flag.Usage)
	case "int":
		set.IntP(flag.Name, flag.Shorthand, value.(int), flag.Usage)
	case "strings":
		set.StringSliceP(flag.Name, flag.Shorthand, value.([]string), flag.Usage)
	}
	if len(flag.Enum) > 0 {
		return command.RegisterFlagCompletionFunc(flag.Name, func(command *cobra.Command, args []string, partial string) ([]string, cobra.ShellCompDirective) {
			return append([]string(nil), flag.Enum...), cobra.ShellCompDirectiveNoFileComp
		})
	}
	return nil
}

func (b *builder) flagValues(command *cobra.Command, definitions map[string]spec.CLIFlag) (map[string]any, error) {
	values := make(map[string]any, len(definitions))
	supplied := make(map[string]bool, len(definitions))
	for name, definition := range definitions {
		flag := command.Flags().Lookup(name)
		if flag == nil {
			flag = command.InheritedFlags().Lookup(name)
		}
		if flag == nil {
			return nil, b.error("configuration", fmt.Errorf("missing compiled flag %s", name))
		}
		var value any
		var err error
		supplied[name] = flag.Changed
		if flag.Changed {
			value, err = parsedFlagValue(command.Flags(), flag, definition.Type)
		} else {
			raw := definition.Default
			env := definition.Env
			if env == "" && (contains(b.contract.Bootstrap.ConfigFlags, "--"+name) || (definition.Shorthand != "" && contains(b.contract.Bootstrap.ConfigFlags, "-"+definition.Shorthand))) {
				env = b.contract.Bootstrap.Env
			}
			if env != "" {
				if fromEnv, ok := b.lookupEnv(env); ok {
					raw, supplied[name] = fromEnv, true
				}
			}
			value, err = spec.ParseCLIFlagValue(definition.Type, raw)
		}
		if err != nil {
			return nil, b.error("usage", fmt.Errorf("flag --%s has an invalid %s value", name, definition.Type))
		}
		if !spec.CLIFlagValueAllowed(value, definition.Enum) {
			return nil, b.error("usage", fmt.Errorf("flag --%s must use an allowed value", name))
		}
		values[name] = value
	}
	for name, definition := range definitions {
		if definition.Required && !supplied[name] {
			return nil, b.error("usage", fmt.Errorf("required flag --%s is missing", name))
		}
		if !supplied[name] {
			continue
		}
		for _, other := range definition.Conflicts {
			if supplied[other] {
				return nil, b.error("usage", fmt.Errorf("flag --%s conflicts with --%s", name, other))
			}
		}
		for _, other := range definition.Requires {
			if !supplied[other] {
				return nil, b.error("usage", fmt.Errorf("flag --%s requires --%s", name, other))
			}
		}
	}
	return values, nil
}

func parsedFlagValue(set *pflag.FlagSet, flag *pflag.Flag, kind string) (any, error) {
	// Cobra merges inherited flags before running the selected command.
	switch kind {
	case "string":
		return set.GetString(flag.Name)
	case "bool":
		return set.GetBool(flag.Name)
	case "int":
		return set.GetInt(flag.Name)
	case "strings":
		value, err := set.GetStringSlice(flag.Name)
		return append([]string(nil), value...), err
	default:
		return nil, fmt.Errorf("unknown flag type")
	}
}

func (b *builder) inputMode(input *spec.CLIInput, args []string) string {
	if input.Mode != "auto" {
		return input.Mode
	}
	if len(args) > 0 {
		return "args"
	}
	if b.options.IsTerminal {
		if input.TerminalFrontendRef != "" {
			return "repl"
		}
		return ""
	}
	if input.Stdin {
		return "stdin"
	}
	return ""
}

func (b *builder) renderHelp(command *cobra.Command) error {
	if err := b.renderHelpTo(command, command.OutOrStdout()); err != nil {
		return b.error("runtime", errors.New("unable to render help"))
	}
	return nil
}

// Help receives a passive snapshot. Giving a template the Cobra object itself
// would allow it to invoke mutating methods, including Execute and ParseFlags.
// Every exposed value comes from authored command metadata or build identity.
type helpUsage struct {
	Name, DisplayName, CommandPath, UseLine, Short, Long, Example string
	Aliases                                                       []string
	Annotations                                                   map[string]string
	NamePadding, CommandPathPadding                               int
	HasExample, HasSubCommands, HasAvailableSubCommands           bool
	HasAvailableLocalFlags, HasAvailableInheritedFlags            bool
	IsAvailableCommand, Runnable                                  bool
	Commands                                                      []helpUsage
	Flags, LocalFlags, LocalNonPersistentFlags, InheritedFlags    helpFlags
}

type helpFlags struct {
	FlagUsages string
}

type helpPage struct {
	helpUsage
	UsageString string
}

func helpView(command *cobra.Command) helpUsage {
	command.InitDefaultHelpFlag()
	view := helpUsage{
		Name: command.Name(), DisplayName: command.DisplayName(), CommandPath: command.CommandPath(),
		UseLine: command.UseLine(), Short: command.Short, Long: command.Long, Example: command.Example,
		Aliases: append([]string(nil), command.Aliases...), Annotations: command.Annotations,
		NamePadding: command.NamePadding(), CommandPathPadding: command.CommandPathPadding(),
		HasExample: command.HasExample(), HasSubCommands: command.HasSubCommands(),
		HasAvailableSubCommands: command.HasAvailableSubCommands(),
		HasAvailableLocalFlags:  command.HasAvailableLocalFlags(), HasAvailableInheritedFlags: command.HasAvailableInheritedFlags(),
		IsAvailableCommand: command.IsAvailableCommand(), Runnable: command.Runnable(),
		Flags:                   helpFlags{FlagUsages: command.Flags().FlagUsages()},
		LocalFlags:              helpFlags{FlagUsages: command.LocalFlags().FlagUsages()},
		LocalNonPersistentFlags: helpFlags{FlagUsages: command.LocalNonPersistentFlags().FlagUsages()},
		InheritedFlags:          helpFlags{FlagUsages: command.InheritedFlags().FlagUsages()},
	}
	for _, child := range command.Commands() {
		view.Commands = append(view.Commands, helpView(child))
	}
	return view
}

func (b *builder) renderHelpTo(command *cobra.Command, output io.Writer) error {
	view := helpView(command)
	var usage bytes.Buffer
	if err := b.usageTemplate.Execute(&usage, view); err != nil {
		return err
	}
	return b.helpTemplate.Execute(output, helpPage{helpUsage: view, UsageString: usage.String()})
}

func (b *builder) completion(root *cobra.Command, shell string, out io.Writer) error {
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(out, true)
	case "zsh":
		err = root.GenZshCompletion(out)
	case "fish":
		err = root.GenFishCompletion(out, true)
	case "powershell":
		err = root.GenPowerShellCompletionWithDesc(out)
	default:
		return b.error("usage", errors.New("unsupported completion shell"))
	}
	if err != nil {
		return b.error("runtime", errors.New("unable to write completion"))
	}
	return nil
}

func (b *builder) error(class string, err error) error {
	codes := b.contract.ExitCodes
	code := codes.Runtime
	switch class {
	case "usage":
		code = codes.Usage
	case "configuration":
		code = codes.Configuration
	case "unsupported":
		code = codes.Unsupported
	case "interrupted":
		code = codes.Interrupted
	}
	return &Error{Class: class, Code: code, Err: err, Prefix: b.contract.Presentation.Errors.Prefix}
}

func argumentRange(args spec.CLIArgs) string {
	if args.Min == args.Max {
		return fmt.Sprintf("%d argument(s)", args.Min)
	}
	if args.Max == -1 {
		return fmt.Sprintf("at least %d argument(s)", args.Min)
	}
	return fmt.Sprintf("%d..%d argument(s)", args.Min, args.Max)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func parseTemplate(name, value string) (*template.Template, error) {
	return template.New(name).Funcs(template.FuncMap{
		"trimTrailingWhitespaces": func(value string) string { return strings.TrimRight(value, " \t\r\n") },
		"trim":                    strings.TrimSpace,
		"trimRightSpace":          func(value string) string { return strings.TrimRight(value, " \t\r\n") },
		"rpad": func(value string, width int) string {
			if len(value) >= width {
				return value
			}
			return value + strings.Repeat(" ", width-len(value))
		},
		"appendIfNotPresent": func(value, suffix string) string {
			if strings.HasSuffix(value, suffix) {
				return value
			}
			return value + suffix
		},
	}).Parse(value)
}
