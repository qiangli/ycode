package agentexec

import (
	"encoding/json"
	"strings"
)

// OutputFormat classifies the format of an external agent's output.
type OutputFormat int

const (
	// FormatUnknown means the output format could not be determined.
	FormatUnknown OutputFormat = iota
	// FormatJSON means the output is a single JSON object.
	FormatJSON
	// FormatJSONArray means the output is a JSON array.
	FormatJSONArray
	// FormatText means the output is plain text.
	FormatText
)

// String returns a human-readable format label.
func (f OutputFormat) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatJSONArray:
		return "json_array"
	case FormatText:
		return "text"
	default:
		return "unknown"
	}
}

// ParsedOutput holds the structured result of parsing an external agent's output.
// Inspired by ralph-claude-code's multi-format response analyzer (3 JSON formats
// with text fallback) and agent-orchestrator's activity signal classification.
type ParsedOutput struct {
	// Format is the detected output format.
	Format OutputFormat

	// Raw is the original output string.
	Raw string

	// JSON holds the parsed JSON data (if format is JSON or JSONArray).
	JSON any

	// SessionID is the session identifier extracted from the output (if present).
	SessionID string

	// Result is the primary text result from the agent.
	Result string

	// IsError indicates whether the output contains error indicators.
	IsError bool

	// ErrorMessage is the extracted error message (if IsError is true).
	ErrorMessage string

	// FilesModified is the count of files changed (if reported by the agent).
	FilesModified int
}

// ParseOutput detects the format of an external agent's output and extracts
// structured fields. Supports JSON object, JSON array, and plain text.
//
// For JSON output, common fields are extracted:
//   - result/output/text → Result
//   - session_id/sessionId → SessionID
//   - is_error/error → IsError + ErrorMessage
//   - files_modified/files_changed → FilesModified
func ParseOutput(raw string) *ParsedOutput {
	parsed := &ParsedOutput{Raw: raw}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		parsed.Format = FormatText
		return parsed
	}

	// Try JSON object.
	if trimmed[0] == '{' {
		var obj map[string]any
		if json.Unmarshal([]byte(trimmed), &obj) == nil {
			parsed.Format = FormatJSON
			parsed.JSON = obj
			extractFields(parsed, obj)
			return parsed
		}
	}

	// Try JSON array.
	if trimmed[0] == '[' {
		var arr []any
		if json.Unmarshal([]byte(trimmed), &arr) == nil {
			parsed.Format = FormatJSONArray
			parsed.JSON = arr
			extractFieldsFromArray(parsed, arr)
			return parsed
		}
	}

	// Fallback to plain text.
	parsed.Format = FormatText
	parsed.Result = trimmed
	return parsed
}

// extractFields pulls common fields from a JSON object.
func extractFields(p *ParsedOutput, obj map[string]any) {
	// Result text.
	for _, key := range []string{"result", "output", "text", "content"} {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				p.Result = s
				break
			}
		}
	}

	// Session ID.
	for _, key := range []string{"session_id", "sessionId", "session"} {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				p.SessionID = s
				break
			}
		}
	}

	// Error detection.
	if v, ok := obj["is_error"]; ok {
		if b, ok := v.(bool); ok {
			p.IsError = b
		}
	}
	if v, ok := obj["error"]; ok {
		if s, ok := v.(string); ok && s != "" {
			p.IsError = true
			p.ErrorMessage = s
		}
	}
	if v, ok := obj["error_message"]; ok {
		if s, ok := v.(string); ok && s != "" {
			p.IsError = true
			p.ErrorMessage = s
		}
	}

	// Files modified.
	for _, key := range []string{"files_modified", "files_changed", "num_files"} {
		if v, ok := obj[key]; ok {
			if f, ok := v.(float64); ok {
				p.FilesModified = int(f)
				break
			}
		}
	}
}

// extractFieldsFromArray handles Claude CLI array format:
// [{type: "system"}, {type: "assistant"}, {type: "result"}]
func extractFieldsFromArray(p *ParsedOutput, arr []any) {
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// Look for result entry.
		if t, ok := obj["type"].(string); ok && t == "result" {
			extractFields(p, obj)
			return
		}

		// Look for system entry with session ID.
		if t, ok := obj["type"].(string); ok && t == "system" {
			if sid, ok := obj["sessionId"].(string); ok && sid != "" {
				p.SessionID = sid
			}
		}
	}

	// If no result entry found, try extracting from last element.
	if p.Result == "" && len(arr) > 0 {
		if obj, ok := arr[len(arr)-1].(map[string]any); ok {
			extractFields(p, obj)
		}
	}
}
