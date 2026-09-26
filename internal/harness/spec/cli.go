package spec

import (
	"encoding/csv"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Interfaces contains declarative projections of the same compiled harness.
type Interfaces struct {
	CLI *CLI `yaml:"cli,omitempty" json:"cli,omitempty"`
}

type CLI struct {
	Identity     CLIIdentity     `yaml:"identity" json:"identity"`
	Bootstrap    CLIBootstrap    `yaml:"bootstrap" json:"bootstrap"`
	Root         CLICommand      `yaml:"root" json:"root"`
	Presentation CLIPresentation `yaml:"presentation" json:"presentation"`
	ExitCodes    CLIExitCodes    `yaml:"exitCodes" json:"exitCodes"`
}
type CLIIdentity struct {
	Name          string `yaml:"name" json:"name"`
	Version       string `yaml:"version" json:"version"`
	Compatibility string `yaml:"compatibility" json:"compatibility"`
}
type CLIBootstrap struct {
	ConfigFlags []string `yaml:"configFlags" json:"configFlags"`
	DefaultFile string   `yaml:"defaultFile" json:"defaultFile"`
	Env         string   `yaml:"env,omitempty" json:"env,omitempty"`
}
type CLICommand struct {
	Name       string       `yaml:"name" json:"name"`
	Usage      string       `yaml:"usage,omitempty" json:"usage,omitempty"`
	Short      string       `yaml:"short" json:"short"`
	Long       string       `yaml:"long,omitempty" json:"long,omitempty"`
	Example    string       `yaml:"example,omitempty" json:"example,omitempty"`
	Aliases    []string     `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Deprecated string       `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	Hidden     bool         `yaml:"hidden,omitempty" json:"hidden,omitempty"`
	Args       CLIArgs      `yaml:"args" json:"args"`
	Flags      []CLIFlag    `yaml:"flags,omitempty" json:"flags,omitempty"`
	Commands   []CLICommand `yaml:"commands,omitempty" json:"commands,omitempty"`
	Dispatch   *CLIDispatch `yaml:"dispatch,omitempty" json:"dispatch,omitempty"`
}
type CLIArgs struct {
	Min  int      `yaml:"min" json:"min"`
	Max  int      `yaml:"max" json:"max"`
	Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}
type CLIFlag struct {
	Name      string   `yaml:"name" json:"name"`
	Shorthand string   `yaml:"shorthand,omitempty" json:"shorthand,omitempty"`
	Type      string   `yaml:"type" json:"type"`
	Default   string   `yaml:"default" json:"default"`
	Usage     string   `yaml:"usage" json:"usage"`
	Scope     string   `yaml:"scope" json:"scope"`
	Env       string   `yaml:"env,omitempty" json:"env,omitempty"`
	Enum      []string `yaml:"enum,omitempty" json:"enum,omitempty"`
	Conflicts []string `yaml:"conflicts,omitempty" json:"conflicts,omitempty"`
	Requires  []string `yaml:"requires,omitempty" json:"requires,omitempty"`
	Required  bool     `yaml:"required,omitempty" json:"required,omitempty"`
}
type CLIDispatch struct {
	Operation   string    `yaml:"operation" json:"operation"`
	Resource    string    `yaml:"resource,omitempty" json:"resource,omitempty"`
	Action      string    `yaml:"action,omitempty" json:"action,omitempty"`
	Columns     []string  `yaml:"columns,omitempty" json:"columns,omitempty"`
	FrontendRef string    `yaml:"frontendRef,omitempty" json:"frontendRef,omitempty"`
	TriggerRef  string    `yaml:"triggerRef,omitempty" json:"triggerRef,omitempty"`
	AgentRef    string    `yaml:"agentRef,omitempty" json:"agentRef,omitempty"`
	PipelineRef string    `yaml:"pipelineRef,omitempty" json:"pipelineRef,omitempty"`
	Input       *CLIInput `yaml:"input,omitempty" json:"input,omitempty"`
	// Scope bounds serve: "all" (the default) serves every network frontend,
	// "frontend" serves only frontendRef.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
}
type CLIInput struct {
	Mode                string `yaml:"mode" json:"mode"`
	Stdin               bool   `yaml:"stdin" json:"stdin"`
	Empty               string `yaml:"empty" json:"empty"`
	TerminalFrontendRef string `yaml:"terminalFrontendRef,omitempty" json:"terminalFrontendRef,omitempty"`
	PayloadKey          string `yaml:"payloadKey" json:"payloadKey"`
	SessionFlag         string `yaml:"sessionFlag,omitempty" json:"sessionFlag,omitempty"`
	// SessionDefault picks the session when the session flag is unset: "new"
	// (the default) starts one, "latest" continues the most recently updated.
	SessionDefault string `yaml:"sessionDefault,omitempty" json:"sessionDefault,omitempty"`
}
type CLIPresentation struct {
	Formats       []string  `yaml:"formats" json:"formats"`
	DefaultFormat string    `yaml:"defaultFormat" json:"defaultFormat"`
	Color         string    `yaml:"color" json:"color"`
	Quiet         bool      `yaml:"quiet" json:"quiet"`
	Redact        string    `yaml:"redact" json:"redact"`
	Help          CLIHelp   `yaml:"help" json:"help"`
	Errors        CLIErrors `yaml:"errors" json:"errors"`
}
type CLIHelp struct {
	Template      string `yaml:"template" json:"template"`
	UsageTemplate string `yaml:"usageTemplate" json:"usageTemplate"`
	Flag          string `yaml:"flag" json:"flag"`
	Shorthand     string `yaml:"shorthand,omitempty" json:"shorthand,omitempty"`
	FlagUsage     string `yaml:"flagUsage" json:"flagUsage"`
}
type CLIErrors struct {
	Prefix string `yaml:"prefix" json:"prefix"`
	Usage  bool   `yaml:"usage" json:"usage"`
}
type CLIExitCodes struct {
	Success       int `yaml:"success" json:"success"`
	Usage         int `yaml:"usage" json:"usage"`
	Configuration int `yaml:"configuration" json:"configuration"`
	Runtime       int `yaml:"runtime" json:"runtime"`
	Unsupported   int `yaml:"unsupported" json:"unsupported"`
	Interrupted   int `yaml:"interrupted" json:"interrupted"`
}

// ParseCLIFlagValue is shared by compilation and execution so defaults,
// environment bindings and explicit arguments have identical type rules.
func ParseCLIFlagValue(kind, value string) (any, error) {
	switch kind {
	case "string":
		return value, nil
	case "bool":
		return strconv.ParseBool(value)
	case "int":
		return strconv.Atoi(value)
	case "strings":
		if value == "" {
			return []string{}, nil
		}
		records, err := csv.NewReader(strings.NewReader(value)).ReadAll()
		if err != nil || len(records) != 1 {
			return nil, fmt.Errorf("strings value must be one CSV record")
		}
		return records[0], nil
	default:
		return nil, fmt.Errorf("unsupported flag type %q", kind)
	}
}

// ValidateCLI also validates documents constructed directly by Go callers.
// Compile additionally checks presence of required fields in the YAML node.
func ValidateCLI(d *Document) error {
	c := d.Spec.Interfaces.CLI
	if c == nil {
		return nil
	}
	fail := func(message string) error { return fmt.Errorf("harness: spec.interfaces.cli: %s", message) }
	if !resourceNamePattern.MatchString(c.Identity.Name) || c.Identity.Version == "" || c.Identity.Compatibility == "" {
		return fail("identity requires a valid name, version and compatibility")
	}
	if c.Identity.Version != "build" {
		return fail("identity.version must be build")
	}
	if c.Root.Name != c.Identity.Name {
		return fail("root.name must match identity.name")
	}
	if len(c.Bootstrap.ConfigFlags) == 0 || c.Bootstrap.DefaultFile == "" {
		return fail("bootstrap.configFlags and defaultFile are required")
	}
	if err := validateCLIEnv(c.Bootstrap.Env); err != nil {
		return fail("bootstrap.env: " + err.Error())
	}
	h := c.Presentation.Help
	if h.Template == "" || h.UsageTemplate == "" || h.Flag != "help" || h.FlagUsage == "" || c.Presentation.Errors.Prefix == "" {
		return fail("presentation requires help.template, usageTemplate, flag=help, flagUsage and errors.prefix")
	}
	if !validCLIShorthand(h.Shorthand) {
		return fail("presentation.help.shorthand must be one ASCII letter")
	}
	p := c.Presentation
	if err := uniqueCLIValues(p.Formats); err != nil || len(p.Formats) == 0 || !stringSliceContains(p.Formats, p.DefaultFormat) {
		return fail("presentation.formats must be unique and include defaultFormat")
	}
	for _, format := range p.Formats {
		if format != "text" && format != "json" && format != "table" {
			return fail("presentation supports formats text, json and table")
		}
	}
	if p.DefaultFormat != "text" || p.Errors.Usage {
		return fail("presentation requires defaultFormat=text and errors.usage=false")
	}
	if p.Color != "never" || p.Quiet || p.Redact != "compiled" {
		return fail("presentation requires color=never, quiet=false and redact=compiled")
	}
	e := c.ExitCodes
	if e.Success != 0 {
		return fail("exitCodes.success must be zero")
	}
	for _, code := range []int{e.Usage, e.Configuration, e.Runtime, e.Unsupported, e.Interrupted} {
		if code < 1 || code > 255 {
			return fail("exitCodes error values must be in 1..255")
		}
	}
	help := CLIFlag{Name: h.Flag, Shorthand: h.Shorthand, Type: "bool", Default: "false", Scope: "inherited", Usage: h.FlagUsage}
	helpCommands := 0
	for _, cmd := range c.Root.Commands {
		if cmd.Dispatch != nil && cmd.Dispatch.Operation == "help" {
			helpCommands++
		}
	}
	if helpCommands != 1 {
		return fail("root.commands must declare exactly one help operation")
	}
	if err := validateCLICommand(d, c.Root, c.Identity.Name, map[string]CLIFlag{help.Name: help}); err != nil {
		return err
	}
	seenConfig := make(map[string]bool)
	configDefinitions := make(map[string][]CLIFlag)
	var collectConfig func(CLICommand)
	collectConfig = func(command CLICommand) {
		for _, flag := range command.Flags {
			configDefinitions["--"+flag.Name] = append(configDefinitions["--"+flag.Name], flag)
			if flag.Shorthand != "" {
				configDefinitions["-"+flag.Shorthand] = append(configDefinitions["-"+flag.Shorthand], flag)
			}
		}
		for _, child := range command.Commands {
			collectConfig(child)
		}
	}
	collectConfig(c.Root)
	for _, name := range c.Bootstrap.ConfigFlags {
		if seenConfig[name] {
			return fail("bootstrap.configFlags contains duplicate " + name)
		}
		seenConfig[name] = true
		flags := configDefinitions[name]
		if len(flags) != 1 || flags[0].Type != "string" || flags[0].Default != c.Bootstrap.DefaultFile {
			return fail("bootstrap config spelling " + name + " must identify one string flag with defaultFile as its default")
		}
	}
	return nil
}

func validateCLICommand(d *Document, cmd CLICommand, path string, inherited map[string]CLIFlag) error {
	fail := func(format string, values ...any) error {
		return fmt.Errorf("harness: spec.interfaces.cli command %s: %s", path, fmt.Sprintf(format, values...))
	}
	if !resourceNamePattern.MatchString(cmd.Name) || cmd.Short == "" {
		return fail("name and short are required; name must be a command token")
	}
	if cmd.Usage != "" && (len(strings.Fields(cmd.Usage)) == 0 || strings.Fields(cmd.Usage)[0] != cmd.Name) {
		return fail("usage must begin with the command name")
	}
	if err := uniqueCLIValues(append([]string{cmd.Name}, cmd.Aliases...)); err != nil {
		return fail("aliases: %v", err)
	}
	for _, alias := range cmd.Aliases {
		if !resourceNamePattern.MatchString(alias) {
			return fail("invalid alias %q", alias)
		}
	}
	if cmd.Args.Min < 0 || cmd.Args.Max < -1 || (cmd.Args.Max != -1 && cmd.Args.Max < cmd.Args.Min) {
		return fail("args requires 0 <= min <= max, or max=-1")
	}
	if err := uniqueCLIValues(cmd.Args.Enum); err != nil {
		return fail("args.enum: %v", err)
	}
	if cmd.Dispatch == nil && len(cmd.Commands) == 0 {
		return fail("leaf command requires dispatch")
	}
	if cmd.Dispatch == nil && (cmd.Args.Min != 0 || cmd.Args.Max != 0) {
		return fail("command without dispatch cannot accept positional arguments")
	}
	if len(cmd.Commands) > 0 && len(cmd.Args.Enum) > 0 {
		return fail("args.enum on a command group makes routes ambiguous")
	}
	active := make(map[string]CLIFlag, len(inherited)+len(cmd.Flags))
	shorthand := make(map[string]string)
	nextInherited := make(map[string]CLIFlag, len(inherited)+len(cmd.Flags))
	for name, flag := range inherited {
		active[name], nextInherited[name] = flag, flag
		if flag.Shorthand != "" {
			shorthand[flag.Shorthand] = name
		}
	}
	for _, flag := range cmd.Flags {
		if !resourceNamePattern.MatchString(flag.Name) || flag.Usage == "" || !validCLIShorthand(flag.Shorthand) {
			return fail("flag %q requires a valid name, usage and an optional one-letter shorthand", flag.Name)
		}
		if flag.Scope != "local" && flag.Scope != "inherited" {
			return fail("flag %s scope must be local or inherited", flag.Name)
		}
		if _, exists := active[flag.Name]; exists {
			return fail("flag %s collides with a local or inherited flag", flag.Name)
		}
		if flag.Shorthand != "" {
			if other, exists := shorthand[flag.Shorthand]; exists {
				return fail("flag %s shorthand collides with %s", flag.Name, other)
			}
			shorthand[flag.Shorthand] = flag.Name
		}
		value, err := ParseCLIFlagValue(flag.Type, flag.Default)
		if err != nil {
			return fail("flag %s has invalid type or default", flag.Name)
		}
		if err := validateCLIEnv(flag.Env); err != nil {
			return fail("flag %s env: %v", flag.Name, err)
		}
		if err := uniqueCLIValues(flag.Enum); err != nil {
			return fail("flag %s enum: %v", flag.Name, err)
		}
		for _, choice := range flag.Enum {
			if _, err := ParseCLIFlagValue(flag.Type, choice); err != nil {
				return fail("flag %s enum has an invalid %s value", flag.Name, flag.Type)
			}
		}
		if len(flag.Enum) > 0 && !CLIFlagValueAllowed(value, flag.Enum) {
			return fail("flag %s default is outside enum", flag.Name)
		}
		active[flag.Name] = flag
		if flag.Scope == "inherited" {
			nextInherited[flag.Name] = flag
		}
	}
	for _, flag := range active {
		for _, refs := range [][]string{flag.Conflicts, flag.Requires} {
			if err := uniqueCLIValues(refs); err != nil {
				return fail("flag %s constraint: %v", flag.Name, err)
			}
			for _, name := range refs {
				if _, exists := active[name]; !exists || name == flag.Name {
					return fail("flag %s constraint references unknown or same flag %q", flag.Name, name)
				}
			}
		}
		for _, name := range flag.Requires {
			if stringSliceContains(flag.Conflicts, name) {
				return fail("flag %s both requires and conflicts with %s", flag.Name, name)
			}
		}
	}
	if cmd.Dispatch != nil {
		if err := validateCLIDispatch(d, *cmd.Dispatch, cmd.Args, active); err != nil {
			return fail("dispatch: %v", err)
		}
	}
	tokens := make(map[string]string)
	for _, child := range cmd.Commands {
		for _, token := range append([]string{child.Name}, child.Aliases...) {
			if !resourceNamePattern.MatchString(token) {
				return fail("invalid command or alias %q", token)
			}
			if other, exists := tokens[token]; exists {
				return fail("command or alias %q collides with %s", token, other)
			}
			tokens[token] = child.Name
		}
		if err := validateCLICommand(d, child, path+" "+child.Name, nextInherited); err != nil {
			return err
		}
	}
	return nil
}

func validateCLIDispatch(d *Document, route CLIDispatch, args CLIArgs, flags map[string]CLIFlag) error {
	if route.PipelineRef != "" {
		if route.Operation != "input" && route.Operation != "serve" && route.Operation != "acp" && route.Operation != "shell" {
			return fmt.Errorf("pipelineRef requires an input, serve, acp or shell operation")
		}
		if _, ok := d.Spec.Pipelines[route.PipelineRef]; !ok {
			return fmt.Errorf("unknown pipelineRef %q", route.PipelineRef)
		}
		if agent, ok := d.Spec.Agents[route.AgentRef]; !ok || agent.PipelineRef != route.PipelineRef {
			return fmt.Errorf("pipelineRef must match the configured agent pipeline")
		}
	}
	routed := route.FrontendRef != "" || route.TriggerRef != "" || route.AgentRef != ""
	if route.Operation != "inspect" && route.Operation != "docs" && route.Operation != "features" && route.Operation != "session" && (route.Resource != "" || route.Action != "") {
		return fmt.Errorf("operation %s does not accept resource or action", route.Operation)
	}
	if len(route.Columns) > 0 {
		if route.Operation != "inspect" || route.Action != "list" {
			return fmt.Errorf("columns require inspect action=list")
		}
		if err := uniqueCLIValues(route.Columns); err != nil {
			return fmt.Errorf("columns: %w", err)
		}
		for _, column := range route.Columns {
			if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*(?:\.[a-zA-Z][a-zA-Z0-9]*)*$`).MatchString(column) {
				return fmt.Errorf("invalid column path %q", column)
			}
		}
	}
	if route.Operation != "input" && route.Input != nil {
		return fmt.Errorf("input policy requires operation=input")
	}
	if route.Scope != "" && (route.Operation != "serve" || (route.Scope != "all" && route.Scope != "frontend")) {
		return fmt.Errorf("scope is serve-only and must be all or frontend")
	}
	switch route.Operation {
	case "input", "serve", "acp":
		if route.FrontendRef == "" || route.TriggerRef == "" || route.AgentRef == "" {
			return fmt.Errorf("operation %s requires frontendRef, triggerRef and agentRef", route.Operation)
		}
		frontend, ok := d.Spec.Frontends[route.FrontendRef]
		if !ok {
			return fmt.Errorf("unknown frontendRef %q", route.FrontendRef)
		}
		trigger, ok := d.Spec.Triggers[route.TriggerRef]
		if !ok || !stringSliceContains(trigger.FrontendRefs, route.FrontendRef) || trigger.Route.AgentRef != route.AgentRef {
			return fmt.Errorf("frontendRef, triggerRef and agentRef must identify one configured route")
		}
		if _, ok := d.Spec.Agents[route.AgentRef]; !ok {
			return fmt.Errorf("unknown agentRef %q", route.AgentRef)
		}
		if route.Operation == "serve" && frontend.Kind != "http" && frontend.Kind != "websocket" && frontend.Kind != "nats" {
			return fmt.Errorf("serve requires a configured network frontend")
		}
		if route.Operation == "acp" && frontend.Kind != "acp" {
			return fmt.Errorf("acp requires a configured acp frontend")
		}
		if route.Operation != "input" {
			break
		}
		input := route.Input
		if input == nil || input.PayloadKey == "" || (input.Empty != "reject" && input.Empty != "help") {
			return fmt.Errorf("input requires payloadKey and empty=reject|help")
		}
		if input.Mode != "auto" && input.Mode != "args" && input.Mode != "stdin" && input.Mode != "repl" {
			return fmt.Errorf("input.mode must be args, stdin, repl or auto")
		}
		// A repl may take one argument: the session it continues (resume
		// SESSION), which needs a session flag to mean anything.
		replSession := input.Mode == "repl" && input.SessionFlag != "" && args.Min == 0 && args.Max == 1
		if (input.Mode == "stdin" || input.Mode == "repl") && (args.Min != 0 || args.Max != 0) && !replSession {
			return fmt.Errorf("stdin/repl input cannot accept arguments (a repl with a session flag may take one: the session)")
		}
		if input.Mode == "stdin" && !input.Stdin {
			return fmt.Errorf("stdin mode requires stdin=true")
		}
		if input.Mode == "args" && input.Stdin {
			return fmt.Errorf("args mode requires stdin=false")
		}
		if frontend.Kind != "one-shot" && frontend.Kind != "repl" && frontend.Kind != "tui" {
			return fmt.Errorf("input requires a local one-shot, repl or tui frontend")
		}
		if input.Mode == "repl" && frontend.Kind != "repl" && frontend.Kind != "tui" {
			return fmt.Errorf("repl mode requires a repl or tui frontend")
		}
		if input.Mode != "repl" && frontend.Kind != "one-shot" {
			return fmt.Errorf("args, stdin and auto input require a one-shot frontend")
		}
		if input.Mode == "repl" && !input.Stdin {
			return fmt.Errorf("repl mode requires stdin=true")
		}
		if input.TerminalFrontendRef != "" {
			terminal, ok := d.Spec.Frontends[input.TerminalFrontendRef]
			if !ok || (terminal.Kind != "repl" && terminal.Kind != "tui") || !stringSliceContains(trigger.FrontendRefs, input.TerminalFrontendRef) || input.Mode != "auto" {
				return fmt.Errorf("terminalFrontendRef requires an auto input route to a configured repl or tui frontend")
			}
		}
		if input.SessionFlag != "" {
			flag, ok := flags[input.SessionFlag]
			if !ok || flag.Type != "string" {
				return fmt.Errorf("sessionFlag must reference an available string flag")
			}
		}
		if input.SessionDefault != "" && input.SessionDefault != "new" && input.SessionDefault != "latest" {
			return fmt.Errorf("input.sessionDefault must be new or latest")
		}
	case "shell":
		if route.AgentRef == "" || route.FrontendRef != "" || route.TriggerRef != "" {
			return fmt.Errorf("shell requires agentRef and no frontendRef or triggerRef")
		}
		if _, ok := d.Spec.Agents[route.AgentRef]; !ok {
			return fmt.Errorf("unknown agentRef %q", route.AgentRef)
		}
	case "inspect":
		if routed || route.Resource == "" || route.Action == "" {
			return fmt.Errorf("inspect requires resource and action and no routing references")
		}
		if err := validateCLIInspection(route, args); err != nil {
			return err
		}
	case "session":
		if routed || route.Resource != "" {
			return fmt.Errorf("session requires an action and no resource or routing references")
		}
		want, ok := sessionActionArgs[route.Action]
		if !ok {
			return fmt.Errorf("session.action must be list, show, export, rename, fork, search, new or status")
		}
		if args.Min != want.Min || args.Max != want.Max {
			return fmt.Errorf("session action %s requires args {min: %d, max: %d}", route.Action, want.Min, want.Max)
		}
	case "readiness":
		if route.FrontendRef != "" || route.TriggerRef != "" {
			return fmt.Errorf("readiness only accepts agentRef")
		}
		if route.AgentRef != "" {
			if _, ok := d.Spec.Agents[route.AgentRef]; !ok {
				return fmt.Errorf("unknown agentRef %q", route.AgentRef)
			}
			if route.AgentRef != d.Spec.Runtime.DefaultAgentRef {
				return fmt.Errorf("readiness.agentRef must identify runtime.defaultAgentRef")
			}
		}
	case "version", "validate", "schema", "docs", "features", "completion", "help", "unsupported":
		if routed {
			return fmt.Errorf("operation %s cannot carry routing references", route.Operation)
		}
		if route.Operation == "features" && route.Action != "list" && route.Action != "readme" && route.Action != "verify" {
			return fmt.Errorf("features.action must be list, readme or verify")
		}
		if route.Operation == "docs" && route.Action != "" && route.Action != "catalog" {
			return fmt.Errorf("docs.action must be empty or catalog")
		}
		if (route.Operation == "docs" || route.Operation == "features") && route.Resource != "" {
			return fmt.Errorf("operation %s does not accept resource", route.Operation)
		}
		if route.Operation == "completion" && (args.Min != 1 || args.Max != 1 || len(args.Enum) != 4 || !stringSliceContains(args.Enum, "bash") || !stringSliceContains(args.Enum, "zsh") || !stringSliceContains(args.Enum, "fish") || !stringSliceContains(args.Enum, "powershell")) {
			return fmt.Errorf("completion requires exactly one shell argument with enum [bash,zsh,fish,powershell]")
		}
	default:
		return fmt.Errorf("unknown operation %q", route.Operation)
	}
	if route.Operation == "version" || route.Operation == "validate" || route.Operation == "schema" || route.Operation == "readiness" || route.Operation == "features" || route.Operation == "shell" || route.Operation == "serve" || route.Operation == "acp" || (route.Operation == "docs" && route.Action == "catalog") {
		if args.Min != 0 || args.Max != 0 {
			return fmt.Errorf("operation %s does not accept positional arguments", route.Operation)
		}
	}
	if route.Operation == "docs" && route.Action == "" && (args.Min != 0 || args.Max != 1) {
		return fmt.Errorf("docs requires zero or one topic argument")
	}
	return nil
}

// sessionActionArgs fixes each session action's positional arguments, so a
// declared command cannot promise arguments the mechanism ignores.
var sessionActionArgs = map[string]CLIArgs{
	"list":   {Min: 0, Max: 0},
	"show":   {Min: 0, Max: 1},  // [SESSION] (default: latest)
	"export": {Min: 0, Max: 1},  // [SESSION]
	"fork":   {Min: 0, Max: 1},  // [SESSION]
	"rename": {Min: 2, Max: -1}, // SESSION TITLE...
	"search": {Min: 1, Max: -1}, // QUERY...
	"new":    {Min: 0, Max: 0},  // start a fresh session
	"status": {Min: 0, Max: 0},  // the terminal's session, else the latest
}

func validateCLIInspection(route CLIDispatch, args CLIArgs) error {
	target := reflect.TypeOf(Document{})
	if route.Resource != "document" {
		target = cliFieldType(reflect.TypeOf(Spec{}), route.Resource)
		if target == nil {
			return fmt.Errorf("unknown inspection resource %q", route.Resource)
		}
	}
	wantArgs := 0
	switch route.Action {
	case "get":
		wantArgs = 1
	case "show":
		if target.Kind() == reflect.Map {
			wantArgs = 1
		}
	case "list":
		if target.Kind() != reflect.Map && route.Resource != "bashy" {
			return fmt.Errorf("inspect.list requires a resource collection or bashy")
		}
		row := target
		if row.Kind() == reflect.Map {
			row = row.Elem()
		}
		for _, column := range route.Columns {
			field := row
			for _, part := range strings.Split(column, ".") {
				field = cliFieldType(field, part)
				if field == nil {
					return fmt.Errorf("unknown inspection column %q in resource %s", column, route.Resource)
				}
			}
		}
	case "path":
		if route.Resource != "document" {
			return fmt.Errorf("inspect.path requires resource=document")
		}
	case "current":
		if route.Resource != "models" {
			return fmt.Errorf("inspect.current requires resource=models")
		}
	default:
		return fmt.Errorf("unknown inspection action %q", route.Action)
	}
	if args.Min != wantArgs || args.Max != wantArgs {
		return fmt.Errorf("inspect.%s for resource %s requires exactly %d argument(s)", route.Action, route.Resource, wantArgs)
	}
	return nil
}

func cliFieldType(parent reflect.Type, name string) reflect.Type {
	if parent == nil {
		return nil
	}
	for parent.Kind() == reflect.Pointer {
		parent = parent.Elem()
	}
	if parent.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < parent.NumField(); i++ {
		field := parent.Field(i)
		if strings.Split(field.Tag.Get("json"), ",")[0] == name {
			return field.Type
		}
	}
	return nil
}

func CLIFlagValueAllowed(value any, enum []string) bool {
	if len(enum) == 0 {
		return true
	}
	if values, ok := value.([]string); ok {
		for _, item := range values {
			if !stringSliceContains(enum, item) {
				return false
			}
		}
		return true
	}
	return stringSliceContains(enum, fmt.Sprint(value))
}

func uniqueCLIValues(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return fmt.Errorf("empty or duplicate value %q", value)
		}
		seen[value] = true
	}
	return nil
}

func validCLIShorthand(value string) bool {
	return value == "" || (len(value) == 1 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')))
}

func validateCLIEnv(value string) error {
	if value != "" && !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(value) {
		return fmt.Errorf("must be an uppercase environment variable name, without interpolation")
	}
	return nil
}

// Required policy values must be authored, including zero/false values that
// otherwise cannot be distinguished from omissions after decoding.
func validateCLINode(node *yaml.Node) error {
	get := func(n *yaml.Node, key string) *yaml.Node {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	cli := get(get(get(node, "spec"), "interfaces"), "cli")
	if cli == nil {
		return nil
	}
	require := func(n *yaml.Node, path string, keys ...string) error {
		for _, key := range keys {
			if get(n, key) == nil {
				return fmt.Errorf("harness: spec.interfaces.cli.%s.%s is required", path, key)
			}
		}
		return nil
	}
	if err := require(cli, "", "identity", "bootstrap", "root", "presentation", "exitCodes"); err != nil {
		return err
	}
	presentation := get(cli, "presentation")
	if err := require(presentation, "presentation", "formats", "defaultFormat", "color", "quiet", "redact", "help", "errors"); err != nil {
		return err
	}
	if err := require(get(presentation, "help"), "presentation.help", "template", "usageTemplate", "flag", "flagUsage"); err != nil {
		return err
	}
	if err := require(get(presentation, "errors"), "presentation.errors", "prefix", "usage"); err != nil {
		return err
	}
	if err := require(get(cli, "exitCodes"), "exitCodes", "success", "usage", "configuration", "runtime", "unsupported", "interrupted"); err != nil {
		return err
	}
	var walk func(*yaml.Node, string) error
	walk = func(n *yaml.Node, path string) error {
		if err := require(n, path, "name", "short", "args"); err != nil {
			return err
		}
		if err := require(get(n, "args"), path+".args", "min", "max"); err != nil {
			return err
		}
		if flags := get(n, "flags"); flags != nil {
			for i, flag := range flags.Content {
				if err := require(flag, fmt.Sprintf("%s.flags[%d]", path, i), "name", "type", "default", "usage", "scope"); err != nil {
					return err
				}
			}
		}
		if input := get(get(n, "dispatch"), "input"); input != nil {
			if err := require(input, path+".dispatch.input", "mode", "stdin", "empty", "payloadKey"); err != nil {
				return err
			}
		}
		if commands := get(n, "commands"); commands != nil {
			for i, child := range commands.Content {
				if err := walk(child, fmt.Sprintf("%s.commands[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(get(cli, "root"), "root")
}
