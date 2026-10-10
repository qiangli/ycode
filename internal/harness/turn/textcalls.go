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
// An entry may also use the canonical nested OpenAI form
// {"type":"function","function":{"name":"bashy","arguments":...}}, and
// arguments may be the JSON-encoded string OpenAI puts on the wire as long
// as it decodes strictly to an object (Sprint 413 story #1846).
//
// Only exact, schema-valid shapes count: prose that merely mentions JSON,
// truncated JSON, an unknown tool, a missing script, or more than one fenced
// block all refuse. Structured tool calls always win — callers must only try
// recovery when the response carries none.
func recoverTextToolCalls(body string) ([]any, bool) {
	calls, _, ok := recoverTextToolCallsDetail(body)
	return calls, ok
}

// recoverTextToolCallsDetail is recoverTextToolCalls that also reports
// whether the document needed the bounded closer repair below to parse, so
// the recovery event can journal it.
//
// Sprint 413 story #1846 (S329 steer/t1-pivot): the model wrote
// {"tool_calls":[{"name":"bashy","arguments":{"script":"..."}]} — the call
// entry is never closed — and the strict parse refused it, so the turn
// ended with the work undone. A document that fails the strict parse only
// because a closer names an outer container while an inner one is still
// open is repaired by closing the inner one first; the result must then
// pass the same strict parse and schema validation. Truncated documents
// (unclosed string or container at end of input) are never repaired.
func recoverTextToolCallsDetail(body string) ([]any, bool, bool) {
	candidate, ok := textToolCallCandidate(body)
	if !ok {
		return nil, false, false
	}
	repaired := false
	var doc map[string]any
	if err := json.Unmarshal([]byte(candidate), &doc); err != nil {
		fixed, changed := repairMismatchedClosers(candidate)
		if !changed {
			return nil, false, false
		}
		doc = nil
		if err := json.Unmarshal([]byte(fixed), &doc); err != nil {
			return nil, false, false
		}
		repaired = true
	}
	if doc == nil {
		return nil, false, false
	}
	var calls []any
	if _, present := doc["tool_calls"]; present {
		calls, ok = buildTextToolCalls(doc["tool_calls"])
	} else if _, present := doc["toolCalls"]; present {
		calls, ok = buildTextToolCalls(doc["toolCalls"])
	} else {
		var call map[string]any
		call, ok = textToolCallFrom(doc)
		calls = []any{call}
	}
	if !ok {
		return nil, false, false
	}
	return calls, repaired, true
}

// maxCloserRepairs bounds how many missing closers one document may have
// inserted; anything further from well-formed refuses.
const maxCloserRepairs = 2

// repairMismatchedClosers scans one JSON document (string- and
// escape-aware) and, where a closer does not match the innermost open
// container but does match one further out, inserts the closers for the
// containers in between. It reports false — refuse — when nothing needed
// inserting, a closer matches no open container, more than maxCloserRepairs
// insertions are needed, or the document ends inside a string or with
// containers still open (truncation is never guessed at).
func repairMismatchedClosers(doc string) (string, bool) {
	var out strings.Builder
	var stack []byte
	inserted := 0
	inString, escaped := false, false
	for index := 0; index < len(doc); index++ {
		ch := doc[index]
		if inString {
			out.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			opener := byte('{')
			if ch == ']' {
				opener = '['
			}
			match := -1
			for depth := len(stack) - 1; depth >= 0; depth-- {
				if stack[depth] == opener {
					match = depth
					break
				}
			}
			if match < 0 {
				return "", false
			}
			for depth := len(stack) - 1; depth > match; depth-- {
				if stack[depth] == '{' {
					out.WriteByte('}')
				} else {
					out.WriteByte(']')
				}
				inserted++
			}
			stack = stack[:match]
		}
		out.WriteByte(ch)
	}
	if inString || len(stack) != 0 || inserted == 0 || inserted > maxCloserRepairs {
		return "", false
	}
	return out.String(), true
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
	if nested, present := entry["function"]; present {
		// The canonical OpenAI form: {"type":"function","function":{...}}.
		// A flat name/arguments beside it is ambiguous and refuses.
		if _, flat := entry["name"]; flat {
			return nil, false
		}
		if _, flat := entry["arguments"]; flat {
			return nil, false
		}
		if kind, present := entry["type"]; present && kind != "function" {
			return nil, false
		}
		entry, ok = nested.(map[string]any)
		if !ok || entry == nil {
			return nil, false
		}
	}
	if name, _ := entry["name"].(string); name != provider.ToolName {
		return nil, false
	}
	args, ok := textToolCallArguments(entry["arguments"])
	if !ok {
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

// textToolCallArguments accepts arguments as an object, or as the
// JSON-encoded string OpenAI carries on the wire when it decodes strictly to
// an object; anything else refuses.
func textToolCallArguments(raw any) (map[string]any, bool) {
	switch value := raw.(type) {
	case map[string]any:
		return value, value != nil
	case string:
		var args map[string]any
		if err := json.Unmarshal([]byte(value), &args); err != nil || args == nil {
			return nil, false
		}
		return args, true
	}
	return nil, false
}
