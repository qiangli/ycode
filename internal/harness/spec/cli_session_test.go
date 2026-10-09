package spec

import (
	"strings"
	"testing"
)

func TestSessionDispatchValidation(t *testing.T) {
	d := &Document{}
	cases := []struct {
		route CLIDispatch
		args  CLIArgs
		want  string
	}{
		{CLIDispatch{Operation: "session", Action: "list"}, CLIArgs{}, ""},
		{CLIDispatch{Operation: "session", Action: "rename"}, CLIArgs{Min: 2, Max: -1}, ""},
		{CLIDispatch{Operation: "session", Action: "rename"}, CLIArgs{Min: 1, Max: 1}, "requires args"},
		{CLIDispatch{Operation: "session", Action: "delete"}, CLIArgs{}, "session.action must be"},
		{CLIDispatch{Operation: "session", Action: "list", Resource: "models"}, CLIArgs{}, "no resource"},
		{CLIDispatch{Operation: "session", Action: "list", AgentRef: "coder"}, CLIArgs{}, "no resource or routing"},
		{CLIDispatch{Operation: "version", Action: "list"}, CLIArgs{}, "does not accept resource or action"},
	}
	for _, c := range cases {
		err := validateCLIDispatch(d, c.route, c.args, nil)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v %+v: err=%v, want %q", c.route, c.args, err, c.want)
		}
	}
}

// init writes one named file; a source with a Paths fallback chain (used by
// the project-instructions AGENTS.md/CLAUDE.md convention) has no single
// file to create, so init.sourceRef must reject it (46f03f676946).
func TestInitSourceRefRejectsPathsFallbackChain(t *testing.T) {
	d := &Document{Spec: Spec{
		Sources: map[string]Source{
			"repository-instructions": {File: &SourceFile{Paths: []string{"AGENTS.md", "CLAUDE.md"}, Required: false}, Limits: SourceLimits{MaxBytes: 8}},
			"template":                {Text: "seed", Limits: SourceLimits{MaxBytes: 8}},
		},
		Contexts: map[string]Context{"main": {Fragments: []ContextFragment{{ID: "repo", SourceRef: "repository-instructions", Role: "system"}}}},
	}}
	route := CLIDispatch{Operation: "init", SourceRef: "repository-instructions", TemplateRef: "template"}
	if err := validateCLIDispatch(d, route, CLIArgs{}, nil); err == nil || !strings.Contains(err.Error(), "single-path file source") {
		t.Fatalf("error = %v", err)
	}
}

func TestServeScopeValidation(t *testing.T) {
	d := &Document{}
	for _, c := range []struct {
		route CLIDispatch
		want  string
	}{
		{CLIDispatch{Operation: "version", Scope: "frontend"}, "scope is serve-only"},
		{CLIDispatch{Operation: "version", Scope: "nearby"}, "scope is serve-only"},
	} {
		if err := validateCLIDispatch(d, c.route, CLIArgs{}, nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err=%v", c.route, err)
		}
	}
}

func TestFrontendUIValidation(t *testing.T) {
	bearer := &FrontendAuth{Mode: "bearer", SecretRef: SecretRef{Provider: "env", Name: "T"}}
	for _, c := range []struct {
		frontend Frontend
		want     string
	}{
		{Frontend{Kind: "http", UI: "chat", Auth: bearer}, ""},
		{Frontend{Kind: "websocket", UI: "chat", Auth: bearer}, "only ui: chat on an http frontend"},
		{Frontend{Kind: "http", UI: "dashboard", Auth: bearer}, "only ui: chat on an http frontend"},
		{Frontend{Kind: "http", UI: "chat"}, "requires bearer auth"},
	} {
		err := validateFrontendUI("web", c.frontend)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v: err=%v, want %q", c.frontend, err, c.want)
		}
	}
}
