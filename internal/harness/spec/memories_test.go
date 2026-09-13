package spec

import (
	"strings"
	"testing"
)

func validMemory() Memory {
	return Memory{Provider: MemoryProviderBashyKB, Recall: RecallPolicy{Rings: []string{"agent", "repo", "host"}, Forms: []string{"note", "page"}, MaxItems: 8, MaxTokens: 3000}, Write: WritePolicy{EveryTurns: 1}}
}

func TestValidateMemoriesAcceptsOnlyBashyKBProvider(t *testing.T) {
	if err := validateMemories(map[string]Memory{"main": validMemory()}); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"memex", "", "kb"} {
		memory := validMemory()
		memory.Provider = provider
		err := validateMemories(map[string]Memory{"main": memory})
		if err == nil || !strings.Contains(err.Error(), "bashy-kb") {
			t.Fatalf("provider %q: err = %v", provider, err)
		}
	}
}

func TestValidateMemoriesFailClosedOnPolicyViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Memory)
		want   string
	}{
		{"no-rings", func(m *Memory) { m.Recall.Rings = nil }, "recall.rings"},
		{"unknown-ring", func(m *Memory) { m.Recall.Rings = []string{"org"} }, "recall.rings"},
		{"duplicate-ring", func(m *Memory) { m.Recall.Rings = []string{"agent", "agent"} }, "repeats"},
		{"no-forms", func(m *Memory) { m.Recall.Forms = nil }, "recall.forms"},
		{"unknown-form", func(m *Memory) { m.Recall.Forms = []string{"fact"} }, "recall.forms"},
		{"zero-items", func(m *Memory) { m.Recall.MaxItems = 0 }, "recall.maxItems"},
		{"zero-tokens", func(m *Memory) { m.Recall.MaxTokens = 0 }, "recall.maxTokens"},
		{"zero-cadence", func(m *Memory) { m.Write.EveryTurns = 0 }, "everyTurns"},
		{"long-cadence", func(m *Memory) { m.Write.EveryTurns = 5 }, "everyTurns"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			memory := validMemory()
			item.mutate(&memory)
			err := validateMemories(map[string]Memory{"main": memory})
			if err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("err = %v, want %q", err, item.want)
			}
		})
	}
}

func bashyRunTemplateFixture(script string, in map[string]string) map[string]Pipeline {
	with := map[string]any{"script": script, "timeoutMs": 5000, "effects": []any{"read"}, "memoryRef": "main"}
	return bashyRunFixture(with, in, map[string]string{"knowledge": "knowledge"})
}

func TestBashyRunScriptResolvesMemoryPolicyPlaceholdersAtCompile(t *testing.T) {
	pipelines := bashyRunTemplateFixture("bashy kb context --json --for {{task}} --rings {{memory.rings}} --forms {{memory.forms}} --budget {{memory.budget}} --k {{memory.k}}", map[string]string{"task": "request"})
	nodes, err := BashyRunNodes(pipelines, map[string]Memory{"main": validMemory()})
	if err != nil {
		t.Fatal(err)
	}
	want := "bashy kb context --json --for {{task}} --rings agent,repo,host --forms note,page --budget 3000 --k 8"
	if nodes["kb"].Script != want {
		t.Fatalf("script = %q, want %q", nodes["kb"].Script, want)
	}
}

func TestBashyRunScriptPlaceholdersFailClosedAtCompile(t *testing.T) {
	memories := map[string]Memory{"main": validMemory()}
	cases := []struct {
		name   string
		script string
		in     map[string]string
		want   string
	}{
		{"unknown-port", "bashy kb context --for {{query}}", map[string]string{"task": "request"}, "does not name an input port"},
		{"unknown-memory-value", "bashy kb context --rings {{memory.scopes}}", nil, "not a compiled memory policy value"},
		{"session-is-allowed-but-empty-name-is-not", "printf {{}}", nil, "does not name an input port"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			_, err := BashyRunNodes(bashyRunTemplateFixture(item.script, item.in), memories)
			if err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("err = %v, want %q", err, item.want)
			}
		})
	}

	t.Run("memory-placeholder-without-memoryRef", func(t *testing.T) {
		pipelines := bashyRunFixture(map[string]any{"script": "bashy kb context --rings {{memory.rings}}", "timeoutMs": 5000, "effects": []any{"read"}}, nil, map[string]string{"knowledge": "knowledge"})
		_, err := BashyRunNodes(pipelines, memories)
		if err == nil || !strings.Contains(err.Error(), "requires with.memoryRef") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown-memoryRef", func(t *testing.T) {
		pipelines := bashyRunTemplateFixture("printf ok", nil)
		_, err := BashyRunNodes(pipelines, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown memory") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("reserved-session-port", func(t *testing.T) {
		pipelines := bashyRunFixture(validBashyRunWith(), map[string]string{"session": "request"}, map[string]string{"knowledge": "knowledge"})
		_, err := BashyRunNodes(pipelines, nil)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCompileRejectsMemexProviderInFixture(t *testing.T) {
	assertFixtureRejects(t,
		"provider: bashy-kb",
		"provider: memex",
		"bashy-kb")
}
