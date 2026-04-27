package task

import (
	"fmt"
	"strings"
)

// TerminalTask implements simple terminal command tasks (hermes TerminalTestEnv pattern).
type TerminalTask struct {
	examples []Example
}

// NewTerminalTask creates terminal tasks with verification.
func NewTerminalTask() *TerminalTask {
	return &TerminalTask{
		examples: []Example{
			{ID: "term-001", Prompt: "Create a file at ~/greeting.txt containing exactly: Hello from ycode", Expected: "Hello from ycode", Metadata: map[string]string{"verify_path": "~/greeting.txt"}},
			{ID: "term-002", Prompt: "Create a file at ~/count.txt containing exactly: 42", Expected: "42", Metadata: map[string]string{"verify_path": "~/count.txt"}},
			{ID: "term-003", Prompt: "Create a file at ~/answer.txt containing exactly: The answer is 7", Expected: "The answer is 7", Metadata: map[string]string{"verify_path": "~/answer.txt"}},
		},
	}
}

func (t *TerminalTask) Name() string { return "terminal" }
func (t *TerminalTask) Len() int     { return len(t.examples) }

func (t *TerminalTask) GetExample(index int) (*Example, error) {
	if index < 0 || index >= len(t.examples) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(t.examples))
	}
	e := t.examples[index]
	return &e, nil
}

// Evaluate checks if the completion mentions creating the file.
// Full verification requires running in a sandbox — this is a heuristic check.
func (t *TerminalTask) Evaluate(example *Example, completion string) (float64, error) {
	expected := example.Expected
	if strings.Contains(completion, expected) {
		return 1.0, nil
	}
	return 0.0, nil
}
