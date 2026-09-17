package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// This independently authored projection replaces only the CLI in a copy of
// the canonical harness. Runtime policies and graph are preserved unchanged.
const fixtureCLI = `
identity: {name: sample, version: build, compatibility: native}
bootstrap: {configFlags: [--file, -f], defaultFile: agent.yaml, env: SAMPLE_CONFIG}
presentation:
  formats: [text, json, table]
  defaultFormat: text
  color: never
  quiet: false
  redact: compiled
  help:
    flag: help
    shorthand: H
    flagUsage: Show YAML help
    template: "{{.Short}}\n{{.UsageString}}"
    usageTemplate: "Use: {{.UseLine}}\n{{range .Commands}}{{if .IsAvailableCommand}}Command: {{.Name}}: {{.Short}}\n{{end}}{{end}}{{range .Aliases}}Alias: {{.}}\n{{end}}"
  errors: {prefix: "sample: ", usage: false}
exitCodes: {success: 0, usage: 22, configuration: 23, runtime: 24, unsupported: 25, interrupted: 126}
root:
  name: sample
  usage: sample [prompt]
  short: Independent YAML interface
  args: {min: 0, max: -1}
  flags:
    - {name: file, shorthand: f, type: string, default: agent.yaml, usage: Harness file, scope: inherited}
    - {name: session, type: string, default: "", usage: Durable session, scope: inherited}
  dispatch:
    operation: input
    frontendRef: one-shot
    triggerRef: interactive-input
    agentRef: coder
    input: {mode: auto, stdin: true, empty: reject, terminalFrontendRef: tui, payloadKey: request, sessionFlag: session}
  commands:
    - name: ask
      aliases: [query]
      usage: ask <message>
      short: Submit arguments
      args: {min: 1, max: -1}
      flags:
        - {name: limit, shorthand: n, type: int, default: "2", usage: Limit, scope: local, env: SAMPLE_LIMIT}
        - {name: json, type: bool, default: "false", usage: JSON, scope: local, conflicts: [table]}
        - {name: table, type: bool, default: "false", usage: Table, scope: local, conflicts: [json]}
        - {name: tags, type: strings, default: "a", usage: Tags, scope: local, enum: [a, b, c]}
        - {name: output, type: string, default: "", usage: Output, scope: local, requires: [json]}
      dispatch:
        operation: input
        frontendRef: one-shot
        triggerRef: interactive-input
        agentRef: coder
        input: {mode: args, stdin: false, empty: reject, payloadKey: request, sessionFlag: session}
    - name: group
      short: Inspect configured state
      args: {min: 0, max: 0}
      flags:
        - {name: verbose, type: bool, default: "false", usage: Verbose, scope: inherited}
      commands:
        - {name: list, short: List state, args: {min: 0, max: 0}, dispatch: {operation: inspect, resource: models, action: list, columns: [id, providerRef]}}
    - name: complete
      short: Generate YAML completion
      args: {min: 1, max: 1, enum: [bash, zsh, fish, powershell]}
      dispatch: {operation: completion}
    - {name: legacy, short: Unsupported surface, args: {min: 0, max: 0}, dispatch: {operation: unsupported}}
    - {name: hidden, hidden: true, short: Hidden state, args: {min: 0, max: 0}, dispatch: {operation: validate}}
    - {name: help, short: Show YAML help, args: {min: 0, max: -1}, dispatch: {operation: help}}
`

func fixtureDocument(t *testing.T) *spec.Document {
	t.Helper()
	path := filepath.Join("..", "..", "..", "examples", "agent.yaml")
	doc, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var cli spec.CLI
	decoder := yaml.NewDecoder(strings.NewReader(fixtureCLI))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cli); err != nil {
		t.Fatal(err)
	}
	doc.Spec.Interfaces.CLI = &cli
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = spec.Compile(path, data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

type capture struct {
	invocations []Invocation
	in          io.Reader
	out, stderr bytes.Buffer
	result      error
}

func makeCommand(t *testing.T, doc *spec.Document, options Options) (*cobra.Command, *capture) {
	t.Helper()
	recorded := &capture{}
	cmd, err := New(doc, func(ctx context.Context, invocation Invocation, streams IO) error {
		recorded.invocations = append(recorded.invocations, invocation)
		recorded.in = streams.In
		if streams.Help == nil {
			return errors.New("selected command help callback missing")
		}
		return recorded.result
	}, options)
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetIn(strings.NewReader("pipe request"))
	cmd.SetOut(&recorded.out)
	cmd.SetErr(&recorded.stderr)
	return cmd, recorded
}

func noEnv(string) (string, bool) { return "", false }

func TestYAMLHelpGoldensAndNoSyntheticSurface(t *testing.T) {
	doc := fixtureDocument(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "Independent YAML interface\nUse: sample [prompt]\nCommand: ask: Submit arguments\nCommand: complete: Generate YAML completion\nCommand: group: Inspect configured state\nCommand: legacy: Unsupported surface\n"},
		{[]string{"help", "group", "list"}, "List state\nUse: sample group list\n"},
		{[]string{"query", "-H"}, "Submit arguments\nUse: sample ask <message>\nAlias: query\n"},
		{[]string{"group"}, "Inspect configured state\nUse: sample group\nCommand: list: List state\n"},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if got := recorded.out.String(); got != test.want {
				t.Fatalf("help:\n%q\nwant:\n%q", got, test.want)
			}
			if len(recorded.invocations) > 0 || recorded.stderr.Len() > 0 {
				t.Fatal("help called dispatcher or wrote a parser diagnostic")
			}
			for _, command := range cmd.Commands() {
				if command.Name() == "completion" || strings.HasPrefix(command.Name(), "__") {
					t.Fatalf("synthetic command %q leaked into tree", command.Name())
				}
			}
			if cmd.Flags().Lookup("version") != nil {
				t.Fatal("synthetic version flag leaked into tree")
			}
			help := cmd.PersistentFlags().Lookup("help")
			if help == nil || help.Usage != "Show YAML help" || help.Shorthand != "H" || help.Annotations[cobra.FlagSetByCobraAnnotation] != nil {
				t.Fatal("Cobra replaced authored help flag")
			}
		})
	}
}

func TestNormalizedDispatchTypesPrecedenceAndRepeatedFlags(t *testing.T) {
	doc := fixtureDocument(t)
	env := func(name string) (string, bool) {
		return map[string]string{"SAMPLE_LIMIT": "9", "SAMPLE_CONFIG": "from-env.yaml"}[name], true
	}
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: env})
	cmd.SetArgs([]string{"-f", "first.yaml", "query", "--file", "last.yaml", "-n", "3", "--limit=5", "--json", "--tags", "a,b", "--tags=c", "hello", "--", "--table", "world"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(recorded.invocations) != 1 {
		t.Fatal("dispatch count")
	}
	got := recorded.invocations[0]
	if !reflect.DeepEqual(got.Command, []string{"sample", "ask"}) || !reflect.DeepEqual(got.Arguments, []string{"hello", "--table", "world"}) {
		t.Fatalf("route/arguments = %#v", got)
	}
	want := map[string]any{"file": "last.yaml", "session": "", "help": false, "limit": 5, "json": true, "table": false, "tags": []string{"a", "b", "c"}, "output": ""}
	if !reflect.DeepEqual(got.Flags, want) {
		t.Fatalf("flags = %#v, want %#v", got.Flags, want)
	}
	if got.Mode != "args" || got.FrontendRef != "one-shot" || got.ConfigFile != doc.Source || got.Dispatch.TriggerRef != "interactive-input" {
		t.Fatalf("input projection = %#v", got)
	}
	if content, err := io.ReadAll(recorded.in); err != nil || string(content) != "pipe request" {
		t.Fatalf("input = %q, %v", content, err)
	}
	if recorded.out.Len() > 0 || recorded.stderr.Len() > 0 {
		t.Fatal("builder wrote runtime output")
	}
}

func TestEnvironmentDefaultsAndInheritedScopes(t *testing.T) {
	doc := fixtureDocument(t)
	lookup := func(name string) (string, bool) {
		value, ok := map[string]string{"SAMPLE_LIMIT": "9", "SAMPLE_CONFIG": "env.yaml"}[name]
		return value, ok
	}
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: lookup})
	cmd.SetArgs([]string{"ask", "hello"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := recorded.invocations[0].Flags; got["limit"] != 9 || got["file"] != "env.yaml" {
		t.Fatalf("environment values = %#v", got)
	}
	cmd, recorded = makeCommand(t, doc, Options{LookupEnv: noEnv})
	cmd.SetArgs([]string{"group", "list", "--verbose", "--session=stable"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := recorded.invocations[0]
	if got.Flags["verbose"] != true || got.Flags["session"] != "stable" || got.Flags["file"] != "agent.yaml" || got.Flags["limit"] != nil {
		t.Fatalf("inherited/local scope = %#v", got.Flags)
	}
	if !reflect.DeepEqual(got.Dispatch.Columns, []string{"id", "providerRef"}) {
		t.Fatal("inspect columns were not preserved")
	}
}

func TestInputModeSelection(t *testing.T) {
	doc := fixtureDocument(t)
	for _, test := range []struct {
		name           string
		terminal       bool
		args           []string
		mode, frontend string
	}{
		{"args precedence", true, []string{"request"}, "args", "one-shot"},
		{"stdin", false, nil, "stdin", "one-shot"},
		{"terminal", true, nil, "repl", "tui"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv, IsTerminal: test.terminal})
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			got := recorded.invocations[0]
			if got.Mode != test.mode || got.FrontendRef != test.frontend {
				t.Fatalf("mode=%q, frontend=%q", got.Mode, got.FrontendRef)
			}
		})
	}
	rootInput := doc.Spec.Interfaces.CLI.Root.Dispatch.Input
	rootInput.Stdin, rootInput.TerminalFrontendRef, rootInput.Empty = false, "", "help"
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorded.out.String(), "Independent YAML interface") || len(recorded.invocations) != 0 {
		t.Fatal("empty input did not render the selected YAML help")
	}
}

func TestUsageErrorsUseAuthoredCodesAndDoNotEchoValues(t *testing.T) {
	doc := fixtureDocument(t)
	tests := [][]string{
		{"ask"}, {"group", "list", "excess"}, {"ask", "--unknown", "hello"},
		{"ask", "--limit=secret-value", "hello"}, {"ask", "--tags=d", "hello"},
		{"ask", "--json", "--table", "hello"}, {"ask", "--output=result", "hello"},
		{"complete", "sh"}, {"help", "absent"}, {"group", "absent"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
			cmd.SetArgs(args)
			err := cmd.Execute()
			var typed *Error
			if !errors.As(err, &typed) || typed.Class != "usage" || ExitCode(err) != 22 {
				t.Fatalf("error = %v, code=%d", err, ExitCode(err))
			}
			if strings.Contains(err.Error(), "secret-value") || !strings.HasPrefix(err.Error(), "sample: ") {
				t.Fatalf("unsafe diagnostic %q", err)
			}
			if len(recorded.invocations) > 0 || recorded.stderr.Len() > 0 || recorded.out.Len() > 0 {
				t.Fatal("invalid invocation reached dispatcher or printed automatic usage")
			}
		})
	}
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: func(string) (string, bool) { return "secret-env-value", true }})
	cmd.SetArgs([]string{"ask", "hello"})
	err := cmd.Execute()
	if ExitCode(err) != 22 || strings.Contains(err.Error(), "secret-env-value") || len(recorded.invocations) != 0 {
		t.Fatalf("environment diagnostic = %v", err)
	}
}

func TestRequiredFlagsAndRuntimeErrorMapping(t *testing.T) {
	doc := fixtureDocument(t)
	doc.Spec.Interfaces.CLI.Root.Commands[0].Flags[0].Required = true
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
	cmd.SetArgs([]string{"ask", "hello"})
	if err := cmd.Execute(); ExitCode(err) != 22 || len(recorded.invocations) != 0 {
		t.Fatalf("required flag = %v", err)
	}
	for _, test := range []struct {
		result error
		class  string
		code   int
	}{
		{errors.New("runtime failed"), "runtime", 24},
		{fmt.Errorf("stopped: %w", context.Canceled), "interrupted", 126},
		{&Error{Class: "configuration", Code: 23, Err: errors.New("configuration failed")}, "configuration", 23},
	} {
		cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
		cmd.SetArgs([]string{"ask", "-n=3", "hello"})
		recorded.result = test.result
		err := cmd.Execute()
		var typed *Error
		if !errors.As(err, &typed) || typed.Class != test.class || ExitCode(err) != test.code || strings.Count(err.Error(), "sample: ") != 1 {
			t.Fatalf("mapped error = %v, code=%d", err, ExitCode(err))
		}
	}
	cmd, recorded = makeCommand(t, doc, Options{LookupEnv: noEnv})
	cmd.SetArgs([]string{"legacy"})
	if err := cmd.Execute(); ExitCode(err) != 25 || len(recorded.invocations) != 0 {
		t.Fatalf("unsupported operation = %v", err)
	}
	if ExitCode(nil) != 0 {
		t.Fatal("success code")
	}
}

func TestCompletionGeneratedFromYAMLTree(t *testing.T) {
	doc := fixtureDocument(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
			cmd.SetArgs([]string{"complete", shell})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if recorded.out.Len() == 0 || !strings.Contains(recorded.out.String(), "sample") || len(recorded.invocations) != 0 {
				t.Fatal("completion was not emitted from the declared CLI")
			}
		})
	}
	cmd, _ := makeCommand(t, doc, Options{LookupEnv: noEnv})
	target, _, err := cmd.Find([]string{"ask"})
	if err != nil {
		t.Fatal(err)
	}
	complete, found := target.GetFlagCompletionFunc("tags")
	if !found {
		t.Fatal("enum flag has no completion")
	}
	values, directive := complete(target, nil, "")
	if !reflect.DeepEqual(values, []string{"a", "b", "c"}) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("enum completion = %v, %v", values, directive)
	}
	cmd, recorded := makeCommand(t, doc, Options{LookupEnv: noEnv})
	cmd.SetArgs([]string{"__complete", "complete", "f"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorded.out.String(), "fish\n") || len(recorded.invocations) != 0 {
		t.Fatalf("positional enum completion = %q", recorded.out.String())
	}
}

func TestInvalidTemplatesAndIndependentCommandTrees(t *testing.T) {
	doc := fixtureDocument(t)
	doc.Spec.Interfaces.CLI.Presentation.Help.Template = "{{unknownFunction}}"
	if _, err := New(doc, func(context.Context, Invocation, IO) error { return nil }, Options{}); ExitCode(err) != 23 {
		t.Fatalf("invalid template error = %v", err)
	}
	doc = fixtureDocument(t)
	doc.Spec.Interfaces.CLI.Presentation.Help.Template = "{{.Execute}}"
	called := false
	_, err := New(doc, func(context.Context, Invocation, IO) error { called = true; return nil }, Options{})
	if called || ExitCode(err) != 23 {
		t.Fatalf("active Cobra template target accepted: called=%v, err=%v", called, err)
	}
	doc = fixtureDocument(t)
	first, one := makeCommand(t, doc, Options{LookupEnv: noEnv, Version: "v1", Commit: "abc"})
	second, two := makeCommand(t, doc, Options{LookupEnv: noEnv, Version: "v2", Commit: "def"})
	first.SetArgs([]string{"ask", "--json", "--tags=b", "first"})
	second.SetArgs([]string{"ask", "second"})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	if one.invocations[0].Flags["json"] != true || two.invocations[0].Flags["json"] != false || !reflect.DeepEqual(two.invocations[0].Flags["tags"], []string{"a"}) || first.Annotations["version"] != "v1" || second.Annotations["commit"] != "def" {
		t.Fatal("command trees share mutable state")
	}
}
