package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONSchemaIncludesNestedGraphLanguage(t *testing.T) {
	data, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	stage := defs["Stage"].(map[string]any)
	properties := stage["properties"].(map[string]any)
	for _, name := range []string{"needs", "use", "call", "when", "repeat", "retry", "switch", "forEach", "fallback"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("stage schema has no %q", name)
		}
	}
}

func TestLoadExample(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	doc, err := Load(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Harness.DefaultAgent != "coder" {
		t.Fatalf("default agent = %q", doc.Harness.DefaultAgent)
	}
	if got := doc.HITL.Rules[0].Effect; len(got) != 3 || got[0] != "destructive" {
		t.Fatalf("decoded effects = %#v", got)
	}
	b, err := doc.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "test-secret") {
		t.Fatal("redacted document contains provider secret")
	}
}

func TestCompileRejectsUnknownField(t *testing.T) {
	_, err := Compile("agent.yaml", []byte("apiVersion: ycode.dev/v1alpha1\nkind: Harness\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsDuplicateKey(t *testing.T) {
	_, err := Compile("agent.yaml", []byte("apiVersion: one\napiVersion: two\n"))
	if err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsAliasAndCustomTag(t *testing.T) {
	for _, input := range []string{
		"apiVersion: &v ycode.dev/v1alpha1\nkind: *v\n",
		"apiVersion: !unsafe ycode.dev/v1alpha1\nkind: Harness\n",
	} {
		if _, err := Compile("agent.yaml", []byte(input)); err == nil {
			t.Fatalf("Compile(%q) succeeded", input)
		}
	}
}

func TestExpandEnvironment(t *testing.T) {
	t.Setenv("HARNESS_SET", "present")
	got, err := expandEnvironment("${HARNESS_SET}/${HARNESS_MISSING:-fallback}")
	if err != nil || got != "present/fallback" {
		t.Fatalf("expand = %q, %v", got, err)
	}
	if _, err := expandEnvironment("${HARNESS_REQUIRED}"); err == nil {
		t.Fatal("missing required environment variable was accepted")
	}
}

func TestResolveSourceConfinesReadableRoots(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	doc := &Document{BaseDir: dir, Harness: Harness{ReadableRoots: []string{"."}}}
	source := ContentSource{File: outside, Required: true}
	if err := doc.resolveSource(&source); err == nil || !strings.Contains(err.Error(), "outside readableRoots") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateStagesRequiresBoundedRepeat(t *testing.T) {
	err := validateStages([]Stage{{ID: "loop", Repeat: &Repeat{Until: "llm.finished", Stages: []Stage{{Use: "llm.call"}}}}}, map[string]struct{}{})
	if err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsStageBeforeItsInputs(t *testing.T) {
	data := loadExampleForContractTest(t)
	data = []byte(strings.Replace(string(data), "    - id: input\n      use: input.read\n", "", 1))
	_, err := Compile(filepath.Join("..", "..", "..", "examples", "agent.yaml"), data)
	if err == nil || (!strings.Contains(err.Error(), `requires unavailable state "input"`) && !strings.Contains(err.Error(), `requires unknown node "input"`)) {
		t.Fatalf("expected state contract error, got %v", err)
	}
}

func TestCompileRejectsUnavailableSessionValue(t *testing.T) {
	data := loadExampleForContractTest(t)
	data = []byte(strings.Replace(string(data), "value: llm.output", "value: bashy.results", 1))
	_, err := Compile(filepath.Join("..", "..", "..", "examples", "agent.yaml"), data)
	if err == nil || !strings.Contains(err.Error(), `references unavailable state "bashy.results"`) {
		t.Fatalf("expected state reference error, got %v", err)
	}
}

func TestCompileRequiresDataProducerAsGraphDependency(t *testing.T) {
	data := loadExampleForContractTest(t)
	data = []byte(strings.Replace(string(data), "needs: [project-context, recall]", "needs: [recall]", 1))
	_, err := Compile(filepath.Join("..", "..", "..", "examples", "agent.yaml"), data)
	if err == nil || !strings.Contains(err.Error(), `requires unavailable state "context"`) {
		t.Fatalf("expected dependency dataflow error, got %v", err)
	}
}

func loadExampleForContractTest(t *testing.T) []byte {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-secret")
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
