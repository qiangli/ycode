package turn

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

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
//     prose reply;
//   - either of the above starting a line after prose and ending the reply;
//   - one bashy({...}) call line (see textToolCallFromMarkup).
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

// textCallRepairs reports which bounded repairs a recovered document needed
// to parse, so the recovery event can journal them.
type textCallRepairs struct {
	Closers bool
	Quotes  bool
}

// recoverTextToolCallsDetail is recoverTextToolCalls that also reports
// which bounded repair, if any, the document needed to parse.
//
// Sprint 413 story #1846 (S329 steer/t1-pivot): the model wrote
// {"tool_calls":[{"name":"bashy","arguments":{"script":"..."}]} — the call
// entry is never closed — and the strict parse refused it, so the turn
// ended with the work undone. A document that fails the strict parse only
// because a closer names an outer container while an inner one is still
// open is repaired by closing the inner one first; the result must then
// pass the same strict parse and schema validation. Truncated documents
// (unclosed string or container at end of input) are never repaired.
//
// Sprint 531 story #60: claude through the model door wrote the call after
// a sentence of prose, left raw double quotes unescaped inside the script
// (a heredoc commit message), or wrote it as a bashy({...}) call line
// followed by a result it made up; the turn ended with nothing run. Those
// shapes recover here under the same validation.
func recoverTextToolCallsDetail(body string) ([]any, textCallRepairs, bool) {
	for _, candidate := range textToolCallCandidates(body) {
		if calls, repairs, ok := recoverTextToolCallDocument(candidate); ok {
			return calls, repairs, true
		}
	}
	if call, ok := textToolCallFromMarkup(body); ok {
		return []any{call}, textCallRepairs{}, true
	}
	return nil, textCallRepairs{}, false
}

// recoverTextToolCallDocument parses one candidate document, strictly first
// and then with the bounded repairs, and validates the calls it carries. A
// document that parses but does not validate refuses without repair.
func recoverTextToolCallDocument(candidate string) ([]any, textCallRepairs, bool) {
	doc, repairs, ok := parseTextToolCallDocument(candidate)
	if !ok {
		return nil, textCallRepairs{}, false
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
		return nil, textCallRepairs{}, false
	}
	return calls, repairs, true
}

// parseTextToolCallDocument tries the strict parse, then each bounded repair
// alone, then both (quotes first: the closer scan needs correct strings).
func parseTextToolCallDocument(candidate string) (map[string]any, textCallRepairs, bool) {
	attempts := []struct {
		repairs textCallRepairs
		fix     func(string) (string, bool)
	}{
		{textCallRepairs{}, func(doc string) (string, bool) { return doc, true }},
		{textCallRepairs{Closers: true}, repairMismatchedClosers},
		{textCallRepairs{Quotes: true}, repairUnescapedQuotes},
		{textCallRepairs{Closers: true, Quotes: true}, func(doc string) (string, bool) {
			quoted, ok := repairUnescapedQuotes(doc)
			if !ok {
				return "", false
			}
			return repairMismatchedClosers(quoted)
		}},
	}
	for _, attempt := range attempts {
		fixed, ok := attempt.fix(candidate)
		if !ok {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(fixed), &doc) == nil && doc != nil {
			return doc, attempt.repairs, true
		}
	}
	return nil, textCallRepairs{}, false
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

// maxQuoteRepairs bounds how many raw quotes one document may have escaped.
const maxQuoteRepairs = 32

// repairUnescapedQuotes escapes each raw double quote inside a JSON string
// that cannot be the string's end, because what follows it (after blanks) is
// not ",", ":", "}", "]" or the end of the document. Models writing a script
// as JSON text leave quotes in heredoc bodies unescaped. It reports false —
// refuse — when nothing needed escaping, more than maxQuoteRepairs did, or
// the document ends inside a string. The result still has to pass the
// strict parse and validation.
func repairUnescapedQuotes(doc string) (string, bool) {
	var out strings.Builder
	inString, escaped := false, false
	inserted := 0
	for index := 0; index < len(doc); index++ {
		ch := doc[index]
		if !inString {
			inString = ch == '"'
			out.WriteByte(ch)
			continue
		}
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			if rest := strings.TrimLeft(doc[index+1:], " \t\r\n"); rest != "" && !strings.ContainsRune(",:}]", rune(rest[0])) {
				out.WriteString(`\"`)
				inserted++
				continue
			}
			inString = false
		}
		out.WriteByte(ch)
	}
	if inString || inserted == 0 || inserted > maxQuoteRepairs {
		return "", false
	}
	return out.String(), true
}

// textToolCallCandidates returns the JSON documents a reply may carry, in
// the order to try them: the whole trimmed text when it opens with an object
// (a trailing prose tail then fails the strict parse), or the single fenced
// block in a reply that has exactly one; then, for a reply that opens with
// prose, the text from its first line that opens with an object to the end
// (prose after that document fails the strict parse).
func textToolCallCandidates(body string) []string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		return []string{trimmed}
	}
	var out []string
	if blocks := fencedBlocks(trimmed); len(blocks) == 1 {
		out = append(out, strings.TrimSpace(stripFenceTag(blocks[0])))
	}
	if at := firstLineOpeningObject(trimmed); at >= 0 {
		out = append(out, strings.TrimSpace(trimmed[at:]))
	}
	return out
}

// firstLineOpeningObject is the offset of the first "{" that opens a line
// (after optional blanks), or -1.
func firstLineOpeningObject(body string) int {
	for offset := 0; offset < len(body); {
		line := body[offset:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if trimmed := strings.TrimLeft(line, " \t\r"); strings.HasPrefix(trimmed, "{") {
			return offset + len(line) - len(trimmed)
		}
		offset += len(line) + 1
	}
	return -1
}

// textToolCallMarkup finds a call line: an optional short lead with no ASCII
// letter or digit (a list dash, an emoji, or its mojibake), then bashy( and
// the arguments object.
var textToolCallMarkup = regexp.MustCompile(`(?m)^([^\n]*?)` + regexp.QuoteMeta(provider.ToolName) + `\(\s*\{`)

// maxMarkupLeadRunes bounds the lead before a call line's tool name.
const maxMarkupLeadRunes = 8

// textToolCallFromMarkup recovers the one transcript-style call line a reply
// may carry, bashy({"script":"..."}), wherever it sits in the reply: the
// arguments object must close with ")" and carry only the bashy tool's own
// argument keys. Text after the call line is ignored: a result written there
// is the model's invention, and running the call replaces it with the real
// one. A call named inside a sentence, two call lines, or anything that is
// not exactly one strict JSON object refuses.
func textToolCallFromMarkup(body string) (map[string]any, bool) {
	matches := textToolCallMarkup.FindAllStringSubmatchIndex(body, -1)
	if len(matches) != 1 {
		return nil, false
	}
	match := matches[0]
	lead := body[match[2]:match[3]]
	if utf8.RuneCountInString(lead) > maxMarkupLeadRunes || strings.IndexFunc(lead, isASCIIAlphanumeric) >= 0 {
		return nil, false
	}
	open := match[1] - 1
	dec := json.NewDecoder(strings.NewReader(body[open:]))
	var args map[string]any
	if dec.Decode(&args) != nil || args == nil {
		return nil, false
	}
	rest := strings.TrimLeft(body[open+int(dec.InputOffset()):], " \t\r\n")
	if !strings.HasPrefix(rest, ")") || !bashyArgumentKeys(args) {
		return nil, false
	}
	return textToolCallFrom(map[string]any{"name": provider.ToolName, "arguments": args})
}

func isASCIIAlphanumeric(r rune) bool {
	return r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
}

// bashyArgumentKeys reports whether every key is a property of the bashy
// tool's input schema: a call line names no tool beyond its prefix, so its
// arguments must be unmistakably bashy's.
func bashyArgumentKeys(args map[string]any) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(provider.BashyTool().InputSchema, &schema) != nil {
		return false
	}
	for key := range args {
		if _, ok := schema.Properties[key]; !ok {
			return false
		}
	}
	return true
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
