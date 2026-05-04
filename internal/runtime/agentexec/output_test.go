package agentexec

import "testing"

func TestParseOutput_EmptyString(t *testing.T) {
	p := ParseOutput("")
	if p.Format != FormatText {
		t.Errorf("format = %v, want text", p.Format)
	}
}

func TestParseOutput_PlainText(t *testing.T) {
	p := ParseOutput("I fixed the bug in main.go by adding the missing import.")
	if p.Format != FormatText {
		t.Errorf("format = %v, want text", p.Format)
	}
	if p.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestParseOutput_JSONObject(t *testing.T) {
	raw := `{"result": "Fixed the bug", "session_id": "sess-123", "files_modified": 3}`
	p := ParseOutput(raw)

	if p.Format != FormatJSON {
		t.Errorf("format = %v, want json", p.Format)
	}
	if p.Result != "Fixed the bug" {
		t.Errorf("result = %q, want %q", p.Result, "Fixed the bug")
	}
	if p.SessionID != "sess-123" {
		t.Errorf("session_id = %q, want %q", p.SessionID, "sess-123")
	}
	if p.FilesModified != 3 {
		t.Errorf("files_modified = %d, want 3", p.FilesModified)
	}
}

func TestParseOutput_JSONObject_SessionId(t *testing.T) {
	raw := `{"output": "Done", "sessionId": "abc-456"}`
	p := ParseOutput(raw)

	if p.Format != FormatJSON {
		t.Errorf("format = %v, want json", p.Format)
	}
	if p.SessionID != "abc-456" {
		t.Errorf("session_id = %q, want %q", p.SessionID, "abc-456")
	}
}

func TestParseOutput_JSONObject_Error(t *testing.T) {
	raw := `{"result": "", "is_error": true, "error_message": "API rate limit exceeded"}`
	p := ParseOutput(raw)

	if !p.IsError {
		t.Error("expected is_error = true")
	}
	if p.ErrorMessage != "API rate limit exceeded" {
		t.Errorf("error_message = %q, want %q", p.ErrorMessage, "API rate limit exceeded")
	}
}

func TestParseOutput_JSONObject_ErrorStringField(t *testing.T) {
	raw := `{"error": "connection refused"}`
	p := ParseOutput(raw)

	if !p.IsError {
		t.Error("expected is_error = true when error field present")
	}
	if p.ErrorMessage != "connection refused" {
		t.Errorf("error_message = %q, want %q", p.ErrorMessage, "connection refused")
	}
}

func TestParseOutput_JSONArray_ClaudeCLI(t *testing.T) {
	raw := `[
		{"type": "system", "sessionId": "session-xyz"},
		{"type": "assistant", "content": "Working on it..."},
		{"type": "result", "result": "All tests pass", "files_modified": 2}
	]`
	p := ParseOutput(raw)

	if p.Format != FormatJSONArray {
		t.Errorf("format = %v, want json_array", p.Format)
	}
	if p.SessionID != "session-xyz" {
		t.Errorf("session_id = %q, want %q", p.SessionID, "session-xyz")
	}
	if p.Result != "All tests pass" {
		t.Errorf("result = %q, want %q", p.Result, "All tests pass")
	}
	if p.FilesModified != 2 {
		t.Errorf("files_modified = %d, want 2", p.FilesModified)
	}
}

func TestParseOutput_JSONArray_NoResultType(t *testing.T) {
	raw := `[{"text": "final output", "files_changed": 1}]`
	p := ParseOutput(raw)

	if p.Format != FormatJSONArray {
		t.Errorf("format = %v, want json_array", p.Format)
	}
	if p.Result == "" {
		t.Error("expected non-empty result from last element fallback")
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	raw := `{invalid json`
	p := ParseOutput(raw)

	if p.Format != FormatText {
		t.Errorf("format = %v, want text (invalid JSON falls back)", p.Format)
	}
}

func TestOutputFormat_String(t *testing.T) {
	tests := []struct {
		f    OutputFormat
		want string
	}{
		{FormatJSON, "json"},
		{FormatJSONArray, "json_array"},
		{FormatText, "text"},
		{FormatUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("OutputFormat(%d).String() = %q, want %q", tt.f, got, tt.want)
		}
	}
}
