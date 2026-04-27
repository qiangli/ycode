package parsers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var toolCallPattern = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)

// HermesParser parses the Hermes XML-style tool call format.
type HermesParser struct{}

func (h *HermesParser) Name() string { return "hermes" }

func (h *HermesParser) Parse(rawOutput string) (string, []ToolCall, error) {
	matches := toolCallPattern.FindAllStringSubmatchIndex(rawOutput, -1)
	if len(matches) == 0 {
		return rawOutput, nil, nil
	}

	var calls []ToolCall
	content := rawOutput

	// Process matches in reverse to preserve indices.
	for i := len(matches) - 1; i >= 0; i-- {
		fullStart, fullEnd := matches[i][0], matches[i][1]
		innerStart, innerEnd := matches[i][2], matches[i][3]
		inner := rawOutput[innerStart:innerEnd]

		// Parse the JSON inside <tool_call>...</tool_call>
		var parsed struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(inner), &parsed); err != nil {
			return "", nil, fmt.Errorf("parse tool call JSON: %w", err)
		}

		calls = append([]ToolCall{{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      parsed.Name,
			Arguments: string(parsed.Arguments),
		}}, calls...)

		// Remove the tag from content.
		content = content[:fullStart] + content[fullEnd:]
	}

	return strings.TrimSpace(content), calls, nil
}
