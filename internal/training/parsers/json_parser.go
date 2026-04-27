package parsers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONParser parses tool calls from JSON arrays in model output (Llama, Qwen style).
type JSONParser struct{}

func (j *JSONParser) Name() string { return "json" }

func (j *JSONParser) Parse(rawOutput string) (string, []ToolCall, error) {
	// Look for JSON array of tool calls.
	idx := strings.Index(rawOutput, "[{")
	if idx < 0 {
		return rawOutput, nil, nil
	}

	// Find matching ]
	endIdx := strings.LastIndex(rawOutput, "}]")
	if endIdx < idx {
		return rawOutput, nil, nil
	}
	jsonStr := rawOutput[idx : endIdx+2]
	content := strings.TrimSpace(rawOutput[:idx])

	var parsed []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return rawOutput, nil, nil // not valid JSON array, treat as plain text
	}

	var calls []ToolCall
	for i, p := range parsed {
		calls = append(calls, ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      p.Name,
			Arguments: string(p.Arguments),
		})
	}

	return content, calls, nil
}
