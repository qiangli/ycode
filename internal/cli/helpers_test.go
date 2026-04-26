package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/usage"
)

func TestToolDetail(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  string
	}{
		{"bash", json.RawMessage(`{"command":"ls -la"}`), "Bash(ls -la)"},
		{"bash", json.RawMessage(`{}`), "Running shell command..."},
		{"read_file", json.RawMessage(`{"file_path":"/foo/bar.go"}`), "Read(bar.go)"},
		{"read_file", json.RawMessage(`{}`), "Reading file..."},
		{"write_file", json.RawMessage(`{"file_path":"/tmp/out.txt"}`), "Write(out.txt)"},
		{"write_file", json.RawMessage(`{}`), "Writing file..."},
		{"edit_file", json.RawMessage(`{"file_path":"/src/main.go"}`), "Edit(main.go)"},
		{"edit_file", json.RawMessage(`{}`), "Editing file..."},
		{"glob_search", json.RawMessage(`{"pattern":"**/*.go"}`), "Glob(**/*.go)"},
		{"glob_search", json.RawMessage(`{}`), "Searching for files..."},
		{"grep_search", json.RawMessage(`{"pattern":"TODO"}`), "Grep(TODO)"},
		{"grep_search", json.RawMessage(`{}`), "Searching file contents..."},
		{"WebFetch", json.RawMessage(`{"url":"https://example.com"}`), "WebFetch(https://example.com)"},
		{"WebFetch", json.RawMessage(`{}`), "Fetching web page..."},
		{"WebSearch", json.RawMessage(`{"query":"go testing"}`), "WebSearch(go testing)"},
		{"WebSearch", json.RawMessage(`{}`), "Searching the web..."},
		{"Agent", json.RawMessage(`{"description":"explore codebase"}`), "Agent(explore codebase)"},
		{"Agent", json.RawMessage(`{}`), "Spawning sub-agent..."},
		{"unknown_tool", json.RawMessage(`{}`), "Tool(unknown_tool)"},
	}

	for _, tt := range tests {
		got := toolDetail(tt.name, tt.input)
		if got != tt.want {
			t.Errorf("toolDetail(%q, %s) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}
}

func TestToolDetail_Truncation(t *testing.T) {
	// Long command should be truncated.
	longCmd := strings.Repeat("x", 200)
	input := json.RawMessage(`{"command":"` + longCmd + `"}`)
	got := toolDetail("bash", input)
	if len(got) > 120 {
		t.Errorf("expected truncated output, got length %d", len(got))
	}
	if !strings.HasSuffix(got, "...)") {
		t.Errorf("expected truncated suffix '...)', got %q", got)
	}
}

func TestToolDetail_MalformedJSON(t *testing.T) {
	// Malformed JSON should not panic — falls through to default params.
	got := toolDetail("bash", json.RawMessage(`not json`))
	if got != "Running shell command..." {
		t.Errorf("expected fallback for malformed JSON, got %q", got)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{150000, "150.0k"},
	}

	for _, tt := range tests {
		got := formatTokenCount(tt.n)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatLLMMetrics(t *testing.T) {
	result := &conversation.TurnResult{
		Duration: 2500 * time.Millisecond,
		Usage: api.Usage{
			InputTokens:  5000,
			OutputTokens: 1200,
		},
	}
	got := formatLLMMetrics(result)
	if !strings.Contains(got, "2.5s") {
		t.Errorf("expected duration '2.5s' in %q", got)
	}
	if !strings.Contains(got, "5.0k") {
		t.Errorf("expected input tokens '5.0k' in %q", got)
	}
	if !strings.Contains(got, "1.2k") {
		t.Errorf("expected output tokens '1.2k' in %q", got)
	}
}

func TestFormatLLMMetrics_Empty(t *testing.T) {
	result := &conversation.TurnResult{}
	got := formatLLMMetrics(result)
	if got != "" {
		t.Errorf("expected empty metrics for zero result, got %q", got)
	}
}

func TestFormatSessionSummary(t *testing.T) {
	tracker := usage.NewTracker()
	tracker.Add(10000, 2000, 0, 0)
	start := time.Now().Add(-30 * time.Second)

	got := formatSessionSummary(tracker, start)
	if !strings.Contains(got, "10.0k") {
		t.Errorf("expected input '10.0k' in %q", got)
	}
	if !strings.Contains(got, "2.0k") {
		t.Errorf("expected output '2.0k' in %q", got)
	}
	if !strings.Contains(got, "12.0k") {
		t.Errorf("expected total '12.0k' in %q", got)
	}
	if !strings.Contains(got, "$") {
		t.Errorf("expected cost in %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m0s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
