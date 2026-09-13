package spec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bashyRunFixture(with map[string]any, in, out map[string]string) map[string]Pipeline {
	return map[string]Pipeline{
		"turn": {
			Inputs:      map[string]string{"request": "ycode.input/v1"},
			State:       map[string]StateSlot{"knowledge": {Type: "ycode.kb-context/v1", Writer: "single"}},
			Outputs:     map[string]string{"output": "ycode.output/v1"},
			Concurrency: 1,
			Nodes:       []Stage{{ID: "kb", Run: Run{Stage: "bashy.run", With: with, In: in, Out: out}}},
		},
	}
}

func validBashyRunWith() map[string]any {
	return map[string]any{"script": "printf ok", "timeoutMs": 5000, "effects": []any{"read"}}
}

func TestBashyRunNodesCompileTypedContract(t *testing.T) {
	nodes, err := BashyRunNodes(bashyRunFixture(validBashyRunWith(), map[string]string{"task": "request"}, map[string]string{"knowledge": "knowledge"}))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := nodes["kb"]
	if !ok {
		t.Fatalf("nodes = %#v", nodes)
	}
	if node.Pipeline != "turn" || node.Script != "printf ok" || node.TimeoutMS != 5000 || len(node.Effects) != 1 || node.Effects[0] != "read" {
		t.Fatalf("node = %#v", node)
	}
	if node.OutPort != "knowledge" || node.OutTarget != "knowledge" || node.OutType != "ycode.kb-context/v1" {
		t.Fatalf("out binding = %#v", node)
	}
}

func TestBashyRunNodesRejectMissingOrInvalidDeclarations(t *testing.T) {
	out := map[string]string{"knowledge": "knowledge"}
	cases := []struct {
		name string
		with map[string]any
		in   map[string]string
		out  map[string]string
		want string
	}{
		{"missing-script", map[string]any{"timeoutMs": 5000, "effects": []any{"read"}}, nil, out, "with.script"},
		{"missing-timeout", map[string]any{"script": "printf ok", "effects": []any{"read"}}, nil, out, "with.timeoutMs"},
		{"zero-timeout", map[string]any{"script": "printf ok", "timeoutMs": 0, "effects": []any{"read"}}, nil, out, "with.timeoutMs"},
		{"missing-effects", map[string]any{"script": "printf ok", "timeoutMs": 5000}, nil, out, "with.effects"},
		{"unknown-effect", map[string]any{"script": "printf ok", "timeoutMs": 5000, "effects": []any{"network"}}, nil, out, "unknown effect"},
		{"unknown-with-key", map[string]any{"script": "printf ok", "timeoutMs": 5000, "effects": []any{"read"}, "shell": "sh"}, nil, out, "does not accept with.shell"},
		{"no-out-port", validBashyRunWith(), nil, nil, "exactly one out port"},
		{"two-out-ports", validBashyRunWith(), nil, map[string]string{"a": "knowledge", "b": "knowledge"}, "exactly one out port"},
		{"nested-out-target", validBashyRunWith(), nil, map[string]string{"knowledge": "knowledge.blocks"}, "root state slot"},
		{"untyped-out-target", validBashyRunWith(), nil, map[string]string{"knowledge": "missing"}, "no declared type"},
		{"invalid-in-port", validBashyRunWith(), map[string]string{"Task": "request"}, out, "input port"},
		{"invalid-out-port", validBashyRunWith(), nil, map[string]string{"KB": "knowledge"}, "out port"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			_, err := BashyRunNodes(bashyRunFixture(item.with, item.in, item.out))
			if err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("err = %v, want %q", err, item.want)
			}
		})
	}
}

func TestBashyRunNodeIDsAreUniqueAcrossPipelines(t *testing.T) {
	pipelines := bashyRunFixture(validBashyRunWith(), nil, map[string]string{"knowledge": "knowledge"})
	other := pipelines["turn"]
	pipelines["persist"] = other
	_, err := BashyRunNodes(pipelines)
	if err == nil || !strings.Contains(err.Error(), "must be unique") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileRejectsBashyRunNodeWithoutTimeout(t *testing.T) {
	raw, err := os.ReadFile(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	mutated := bytes.Replace(raw, []byte("              timeoutMs: 5000\n"), nil, 1)
	if bytes.Equal(mutated, raw) {
		t.Fatal("fixture does not carry the example bashy.run timeout")
	}
	if _, err := Compile(filepath.Join(filepath.Dir(fixturePath()), "agent.yaml"), mutated); err == nil || !strings.Contains(err.Error(), "with.timeoutMs") {
		t.Fatalf("err = %v", err)
	}
}

func TestCanonicalFixtureDeclaresBashyRunExample(t *testing.T) {
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := BashyRunNodes(doc.Spec.Pipelines)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := nodes["workspace-probe"]
	if !ok || node.OutPort != "probe" || node.OutType != "string" {
		t.Fatalf("workspace-probe = %#v (present %v)", node, ok)
	}
}
