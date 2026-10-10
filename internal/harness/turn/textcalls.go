package turn

import (
	"encoding/json"
	"strings"

	"github.com/qiangli/ycode/internal/harness/provider"
)

// textToolCallRecoveredEvent is journaled when a reply with no structured
// tool calls carries model-written JSON that normalizes into real ones
// (Sprint 412 story #1819: opus answered with TEXT bodies holding tool
// calls and the turn finished with no diff). The script itself stays in the
// response text; the event carries only the count.
const textToolCallRecoveredEvent = "llm.text_tool_call_recovered"

// recoverTextToolCalls normalizes provider text that IS a model-written tool
// call back into real tool calls. Accepted shapes, and only these:
//
//   - a well-formed object with a tool_calls (or toolCalls) array whose every
//     entry names the declared bashy tool with an arguments object carrying a
//     non-empty script string;
//   - a single {"name":"bashy","arguments":{...}} object of the same form;
//   - either of the above as the single fenced code block in an otherwise
//     prose reply.
//
// Only exact, schema-valid shapes count: prose that merely mentions JSON,
// truncated JSON, an unknown tool, a missing script, or more than one fenced
// block all refuse. Structured tool calls always win — callers must only try
// recovery when the response carries none.
func recoverTextToolCalls(body string) ([]any, bool) {
	candidate, ok := textToolCallCandidate(body)
	if !ok {
		return nil, false
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(candidate), &doc); err != nil || doc == nil {
		return nil, false
	}
	if _, present := doc["tool_calls"]; present {
		return buildTextToolCalls(doc["tool_calls"])
	}
	if _, present := doc["toolCalls"]; present {
		return buildTextToolCalls(doc["toolCalls"])
	}
	call, ok := textToolCallFrom(doc)
	if !ok {
		return nil, false
	}
	return []any{call}, true
}

// textToolCallCandidate extracts the one JSON document a reply may carry:
// the whole trimmed text when it opens with an object (a trailing prose tail
// then fails the strict parse below), or the single fenced block in a reply
// that has exactly one.
func textToolCallCandidate(body string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "{") {
		return trimmed, true
	}
	blocks := fencedBlocks(trimmed)
	if len(blocks) != 1 {
		return "", false
	}
	return strings.TrimSpace(stripFenceTag(blocks[0])), true
}

// fencedBlocks returns the bodies of every ``` ... ``` span in order; an
// unclosed fence contributes nothing.
func fencedBlocks(body string) []string {
	var out []string
	rest := body
	for {
		start := strings.Index(rest, "```")
		if start < 0 {
			return out
		}
		rest = rest[start+3:]
		end := strings.Index(rest, "```")
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end+3:]
	}
}

// stripFenceTag drops an optional language tag line (```json) so the block
// starts at the document. The strict parse decides what is JSON.
func stripFenceTag(block string) string {
	trimmed := strings.TrimSpace(block)
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	if index := strings.Index(trimmed, "\n"); index >= 0 {
		return strings.TrimSpace(trimmed[index+1:])
	}
	return trimmed
}

// buildTextToolCalls validates every entry of a tool_calls array: one bad
// entry refuses the whole body rather than executing a prefix of it.
func buildTextToolCalls(raw any) ([]any, bool) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, false
	}
	calls := make([]any, 0, len(items))
	for _, item := range items {
		call, ok := textToolCallFrom(item)
		if !ok {
			return nil, false
		}
		calls = append(calls, call)
	}
	return calls, true
}

// textToolCallFrom validates one model-written call: exactly the declared
// bashy tool, arguments as an object, script a non-empty string. The whole
// arguments object becomes the call input so timeout_ms/digest survive.
func textToolCallFrom(raw any) (map[string]any, bool) {
	entry, ok := raw.(map[string]any)
	if !ok || entry == nil {
		return nil, false
	}
	if name, _ := entry["name"].(string); name != provider.ToolName {
		return nil, false
	}
	args, ok := entry["arguments"].(map[string]any)
	if !ok || args == nil {
		return nil, false
	}
	script, _ := args["script"].(string)
	if strings.TrimSpace(script) == "" {
		return nil, false
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return map[string]any{
		"id":    stableID("text-tool-call", string(encoded)),
		"name":  provider.ToolName,
		"input": args,
	}, true
}
