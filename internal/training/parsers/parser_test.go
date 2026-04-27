package parsers

import "testing"

func TestHermesParser_NoToolCalls(t *testing.T) {
	p := &HermesParser{}
	content, calls, err := p.Parse("Hello, world!")
	if err != nil {
		t.Fatal(err)
	}
	if content != "Hello, world!" {
		t.Errorf("unexpected content: %q", content)
	}
	if len(calls) != 0 {
		t.Errorf("expected no calls, got %d", len(calls))
	}
}

func TestHermesParser_SingleToolCall(t *testing.T) {
	p := &HermesParser{}
	raw := `Let me check that. <tool_call>{"name": "Bash", "arguments": {"cmd": "ls"}}</tool_call>`
	content, calls, err := p.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if content != "Let me check that." {
		t.Errorf("unexpected content: %q", content)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Errorf("expected tool name 'Bash', got %q", calls[0].Name)
	}
}

func TestHermesParser_MultipleToolCalls(t *testing.T) {
	p := &HermesParser{}
	raw := `First <tool_call>{"name": "Read", "arguments": {"path": "/tmp/a"}}</tool_call> then <tool_call>{"name": "Write", "arguments": {"path": "/tmp/b"}}</tool_call> done`
	content, calls, err := p.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "Read" {
		t.Errorf("first call: expected 'Read', got %q", calls[0].Name)
	}
	if calls[1].Name != "Write" {
		t.Errorf("second call: expected 'Write', got %q", calls[1].Name)
	}
	if content != "First  then  done" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestJSONParser_Basic(t *testing.T) {
	p := &JSONParser{}
	raw := `I'll do this: [{"name": "Bash", "arguments": {"cmd": "echo hi"}}]`
	content, calls, err := p.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if content != "I'll do this:" {
		t.Errorf("unexpected content: %q", content)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Errorf("expected tool name 'Bash', got %q", calls[0].Name)
	}
}

func TestJSONParser_NoJSON(t *testing.T) {
	p := &JSONParser{}
	content, calls, err := p.Parse("Just plain text.")
	if err != nil {
		t.Fatal(err)
	}
	if content != "Just plain text." {
		t.Errorf("unexpected content: %q", content)
	}
	if len(calls) != 0 {
		t.Errorf("expected no calls, got %d", len(calls))
	}
}

func TestGet_Existing(t *testing.T) {
	p, err := Get("hermes")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "hermes" {
		t.Errorf("expected 'hermes', got %q", p.Name())
	}

	p, err = Get("json")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "json" {
		t.Errorf("expected 'json', got %q", p.Name())
	}
}

func TestGet_NonExistent(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown parser")
	}
}
