package spec

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func cliDocument(t *testing.T) *Document {
	t.Helper()
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	doc.Spec.Interfaces.CLI = &CLI{
		Identity:  CLIIdentity{Name: "sample", Version: "build", Compatibility: "native"},
		Bootstrap: CLIBootstrap{ConfigFlags: []string{"--file", "-f"}, DefaultFile: "agent.yaml"},
		Presentation: CLIPresentation{
			Formats: []string{"text", "json", "table"}, DefaultFormat: "text", Color: "never", Redact: "compiled",
			Help:   CLIHelp{Template: "{{.Short}}\n{{.UsageString}}", UsageTemplate: "{{.UseLine}}\n", Flag: "help", Shorthand: "h", FlagUsage: "Show help"},
			Errors: CLIErrors{Prefix: "error: "},
		},
		ExitCodes: CLIExitCodes{Success: 0, Usage: 2, Configuration: 3, Runtime: 1, Unsupported: 4, Interrupted: 130},
		Root: CLICommand{
			Name: "sample", Short: "Sample interface", Args: CLIArgs{Max: -1},
			Flags: []CLIFlag{{Name: "file", Shorthand: "f", Type: "string", Default: "agent.yaml", Usage: "Harness file", Scope: "inherited"}},
			Dispatch: &CLIDispatch{
				Operation: "input", FrontendRef: "one-shot", TriggerRef: "interactive-input", AgentRef: "coder",
				Input: &CLIInput{Mode: "auto", Stdin: true, Empty: "reject", TerminalFrontendRef: "tui", PayloadKey: "request"},
			},
			Commands: []CLICommand{
				{Name: "status", Short: "Check configuration", Args: CLIArgs{}, Dispatch: &CLIDispatch{Operation: "validate"}},
				{Name: "help", Short: "Show help", Args: CLIArgs{Max: -1}, Dispatch: &CLIDispatch{Operation: "help"}},
			},
		},
	}
	return doc
}

func TestCLIContractCompilesWithoutReplacingHarness(t *testing.T) {
	doc := cliDocument(t)
	doc.Spec.Interfaces.CLI.Root.Dispatch.PipelineRef = doc.Spec.Agents["coder"].PipelineRef
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Compile(fixturePath(), data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Interfaces.CLI.Root.Name != "sample" || got.Spec.Runtime.DefaultAgentRef != "coder" {
		t.Fatal("CLI projection changed the harness")
	}
	if got.Spec.Interfaces.CLI.Root.Dispatch.PipelineRef != got.Spec.Agents["coder"].PipelineRef {
		t.Fatal("pipeline assertion was not preserved")
	}
	withoutCLI := *doc
	withoutCLI.Spec.Interfaces = Interfaces{}
	data, err = yaml.Marshal(withoutCLI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(fixturePath(), data); err != nil {
		t.Fatalf("existing documents without CLI were rejected: %v", err)
	}
}

func TestCLIContractRejectsInvalidPolicy(t *testing.T) {
	tests := []struct {
		name string
		edit func(*CLI)
		want string
	}{
		{"identity", func(c *CLI) { c.Identity.Name = "bad name" }, "identity"},
		{"root identity", func(c *CLI) { c.Root.Name = "different" }, "root.name"},
		{"bootstrap", func(c *CLI) { c.Bootstrap.ConfigFlags = []string{"--absent"} }, "config spelling"},
		{"exit code", func(c *CLI) { c.ExitCodes.Usage = 0 }, "1..255"},
		{"help policy", func(c *CLI) { c.Presentation.Help.Template = "" }, "presentation"},
		{"format policy", func(c *CLI) { c.Presentation.Formats = []string{"xml"} }, "formats"},
		{"color policy", func(c *CLI) { c.Presentation.Color = "auto" }, "color=never"},
		{"quiet policy", func(c *CLI) { c.Presentation.Quiet = true }, "quiet=false"},
		{"redaction policy", func(c *CLI) { c.Presentation.Redact = "none" }, "redact=compiled"},
		{"argument range", func(c *CLI) { c.Root.Args = CLIArgs{Min: 2, Max: 1} }, "args requires"},
		{"usage token", func(c *CLI) { c.Root.Usage = "foreign [text]" }, "usage must begin"},
		{"duplicate alias", func(c *CLI) { c.Root.Commands[0].Aliases = []string{"help"} }, "collides"},
		{"duplicate inherited flag", func(c *CLI) { c.Root.Commands[0].Flags = []CLIFlag{c.Root.Flags[0]} }, "collides"},
		{"reserved help flag", func(c *CLI) { c.Root.Flags[0].Name = "help" }, "collides"},
		{"inherited shorthand", func(c *CLI) {
			c.Root.Commands[0].Flags = []CLIFlag{{Name: "other", Shorthand: "f", Type: "string", Usage: "Other", Scope: "local"}}
		}, "shorthand collides"},
		{"flag type", func(c *CLI) { c.Root.Flags[0].Type = "float" }, "invalid type"},
		{"flag scope", func(c *CLI) { c.Root.Flags[0].Scope = "global" }, "scope"},
		{"flag enum", func(c *CLI) { c.Root.Flags[0].Enum = []string{"other.yaml"} }, "outside enum"},
		{"flag default", func(c *CLI) { c.Root.Flags[0].Type, c.Root.Flags[0].Default = "int", "one" }, "invalid type or default"},
		{"multiline CSV default", func(c *CLI) { c.Root.Flags[0].Type, c.Root.Flags[0].Default = "strings", "one\ntwo" }, "invalid type or default"},
		{"flag env", func(c *CLI) { c.Root.Flags[0].Env = "TOKEN=$(secret)" }, "environment variable"},
		{"flag constraints", func(c *CLI) { c.Root.Flags[0].Requires = []string{"absent"} }, "constraint references"},
		{"operation", func(c *CLI) { c.Root.Commands[0].Dispatch.Operation = "execute" }, "unknown operation"},
		{"operation shape", func(c *CLI) { c.Root.Commands[0].Dispatch.AgentRef = "coder" }, "routing references"},
		{"frontend trigger coherence", func(c *CLI) { c.Root.Dispatch.AgentRef = "reviewer" }, "one configured route"},
		{"frontend kind", func(c *CLI) { c.Root.Dispatch.FrontendRef = "http" }, "local one-shot"},
		{"input stdin", func(c *CLI) {
			c.Root.Dispatch.Input.Mode, c.Root.Dispatch.Input.Stdin, c.Root.Args.Max = "stdin", false, 0
		}, "stdin=true"},
		{"input empty", func(c *CLI) { c.Root.Dispatch.Input.Empty = "continue" }, "empty=reject|help"},
		{"input session", func(c *CLI) { c.Root.Dispatch.Input.SessionFlag = "absent" }, "sessionFlag"},
		{"columns", func(c *CLI) { c.Root.Commands[0].Dispatch.Columns = []string{"id"} }, "columns require"},
		{"resource", func(c *CLI) {
			c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "inspect", Resource: "absent", Action: "list"}
		}, "unknown inspection resource"},
		{"inspect action", func(c *CLI) {
			c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "inspect", Resource: "models", Action: "select"}
		}, "unknown inspection action"},
		{"inspect column", func(c *CLI) {
			c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "inspect", Resource: "models", Action: "list", Columns: []string{"missing"}}
		}, "unknown inspection column"},
		{"inspect args", func(c *CLI) {
			c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "inspect", Resource: "models", Action: "show"}
		}, "requires exactly 1"},
		{"features action", func(c *CLI) { c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "features", Action: "select"} }, "features.action"},
		{"docs action", func(c *CLI) { c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "docs", Action: "select"} }, "docs.action"},
		{"readiness route", func(c *CLI) { c.Root.Commands[0].Dispatch = &CLIDispatch{Operation: "readiness", AgentRef: "reviewer"} }, "defaultAgentRef"},
		{"pipeline shape", func(c *CLI) { c.Root.Commands[0].Dispatch.PipelineRef = "main" }, "pipelineRef requires"},
		{"pipeline mismatch", func(c *CLI) { c.Root.Dispatch.PipelineRef = "compact-state" }, "configured agent pipeline"},
		{"leaf", func(c *CLI) { c.Root.Commands[0].Dispatch = nil }, "leaf command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := cliDocument(t)
			test.edit(doc.Spec.Interfaces.CLI)
			err := ValidateCLI(doc)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCLICompileRejectsMissingFalseZeroAndTypedReferences(t *testing.T) {
	data, err := yaml.Marshal(cliDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := yaml.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"success", "quiet", "usage", "stdin", "max"} {
		t.Run(missing, func(t *testing.T) {
			var document map[string]any
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			cli := document["spec"].(map[string]any)["interfaces"].(map[string]any)["cli"].(map[string]any)
			presentation := cli["presentation"].(map[string]any)
			root := cli["root"].(map[string]any)
			switch missing {
			case "success":
				delete(cli["exitCodes"].(map[string]any), missing)
			case "quiet":
				delete(presentation, missing)
			case "usage":
				delete(presentation["errors"].(map[string]any), missing)
			case "stdin":
				delete(root["dispatch"].(map[string]any)["input"].(map[string]any), missing)
			case "max":
				delete(root["args"].(map[string]any), missing)
			}
			mutated, err := yaml.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(fixturePath(), mutated); err == nil || !strings.Contains(err.Error(), "required") {
				t.Fatalf("missing %s accepted: %v", missing, err)
			}
		})
	}
	for _, ref := range []string{"triggerRef", "frontendRef", "terminalFrontendRef", "agentRef", "pipelineRef"} {
		t.Run(ref, func(t *testing.T) {
			doc := cliDocument(t)
			switch ref {
			case "triggerRef":
				doc.Spec.Interfaces.CLI.Root.Dispatch.TriggerRef = "absent"
			case "frontendRef":
				doc.Spec.Interfaces.CLI.Root.Dispatch.FrontendRef = "absent"
			case "terminalFrontendRef":
				doc.Spec.Interfaces.CLI.Root.Dispatch.Input.TerminalFrontendRef = "absent"
			case "agentRef":
				doc.Spec.Interfaces.CLI.Root.Dispatch.AgentRef = "absent"
			case "pipelineRef":
				doc.Spec.Interfaces.CLI.Root.Dispatch.PipelineRef = "absent"
			}
			encoded, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(fixturePath(), encoded); err == nil || !strings.Contains(err.Error(), "references unknown") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCLIJSONSchemaIsStrictAndRecursive(t *testing.T) {
	data, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	for _, name := range []string{"Interfaces", "CLI", "CLICommand", "CLIFlag", "CLIDispatch", "CLIInput", "CLIExitCodes", "CLIPresentation"} {
		if defs[name].(map[string]any)["additionalProperties"] != false {
			t.Fatalf("%s is not strict", name)
		}
	}
	command := defs["CLICommand"].(map[string]any)["properties"].(map[string]any)
	if command["commands"].(map[string]any)["items"].(map[string]any)["$ref"] != "#/$defs/CLICommand" {
		t.Fatal("command tree schema is not recursive")
	}
}
