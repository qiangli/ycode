package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/session"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

func TestAnalyzeMessageStructure_NoToolCalls(t *testing.T) {
	msgs := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hello"}}},
		{Role: api.RoleAssistant, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hi"}}},
	}
	ms := analyzeMessageStructure(msgs)

	if ms.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", ms.MessageCount)
	}
	if ms.RoleSequence != "user,assistant" {
		t.Errorf("RoleSequence = %q, want %q", ms.RoleSequence, "user,assistant")
	}
	if len(ms.ToolUseIDs) != 0 {
		t.Errorf("ToolUseIDs = %v, want empty", ms.ToolUseIDs)
	}
	if len(ms.OrphanUseIDs) != 0 {
		t.Errorf("OrphanUseIDs = %v, want empty", ms.OrphanUseIDs)
	}
}

func TestAnalyzeMessageStructure_MatchedToolCalls(t *testing.T) {
	msgs := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "run ls"}}},
		{Role: api.RoleAssistant, Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: "running"},
			{Type: api.ContentTypeToolUse, ID: "t1", Name: "bash", Input: json.RawMessage(`{}`)},
		}},
		{Role: api.RoleUser, Content: []api.ContentBlock{
			{Type: api.ContentTypeToolResult, ToolUseID: "t1", Content: "file1\nfile2"},
		}},
	}
	ms := analyzeMessageStructure(msgs)

	if len(ms.ToolUseIDs) != 1 || ms.ToolUseIDs[0] != "t1" {
		t.Errorf("ToolUseIDs = %v, want [t1]", ms.ToolUseIDs)
	}
	if len(ms.ToolResultIDs) != 1 || ms.ToolResultIDs[0] != "t1" {
		t.Errorf("ToolResultIDs = %v, want [t1]", ms.ToolResultIDs)
	}
	if len(ms.OrphanUseIDs) != 0 {
		t.Errorf("OrphanUseIDs = %v, want empty", ms.OrphanUseIDs)
	}
	if len(ms.OrphanResultIDs) != 0 {
		t.Errorf("OrphanResultIDs = %v, want empty", ms.OrphanResultIDs)
	}
}

func TestAnalyzeMessageStructure_OrphanToolUse(t *testing.T) {
	// Simulates the pause/resume bug: tool_use without matching tool_result
	msgs := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "run ls"}}},
		{Role: api.RoleAssistant, Content: []api.ContentBlock{
			{Type: api.ContentTypeToolUse, ID: "bash:66", Name: "bash", Input: json.RawMessage(`{}`)},
		}},
		// Injected context (the bug pattern)
		{Role: api.RoleAssistant, Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: "Noted"},
		}},
		{Role: api.RoleUser, Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: "focus on docker"},
		}},
		// No tool_result for bash:66
	}
	ms := analyzeMessageStructure(msgs)

	if len(ms.OrphanUseIDs) != 1 || ms.OrphanUseIDs[0] != "bash:66" {
		t.Errorf("OrphanUseIDs = %v, want [bash:66]", ms.OrphanUseIDs)
	}
	if ms.RoleSequence != "user,assistant,assistant,user" {
		t.Errorf("RoleSequence = %q, want %q", ms.RoleSequence, "user,assistant,assistant,user")
	}
}

func TestAnalyzeMessageStructure_OrphanToolResult(t *testing.T) {
	msgs := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{
			{Type: api.ContentTypeToolResult, ToolUseID: "nonexistent", Content: "output"},
		}},
	}
	ms := analyzeMessageStructure(msgs)

	if len(ms.OrphanResultIDs) != 1 || ms.OrphanResultIDs[0] != "nonexistent" {
		t.Errorf("OrphanResultIDs = %v, want [nonexistent]", ms.OrphanResultIDs)
	}
}

func TestAnalyzeMessageStructure_MultipleToolCalls(t *testing.T) {
	msgs := []api.Message{
		{Role: api.RoleAssistant, Content: []api.ContentBlock{
			{Type: api.ContentTypeToolUse, ID: "t1", Name: "bash"},
			{Type: api.ContentTypeToolUse, ID: "t2", Name: "read"},
		}},
		{Role: api.RoleUser, Content: []api.ContentBlock{
			{Type: api.ContentTypeToolResult, ToolUseID: "t1", Content: "ok"},
			// t2 missing
		}},
	}
	ms := analyzeMessageStructure(msgs)

	if len(ms.OrphanUseIDs) != 1 || ms.OrphanUseIDs[0] != "t2" {
		t.Errorf("OrphanUseIDs = %v, want [t2]", ms.OrphanUseIDs)
	}
}

func TestClassifyAPIError_OrphanToolCall(t *testing.T) {
	errMsg := `stream: API error 400: {"error":{"message":"Invalid request: an assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. The following tool_call_ids did not have response messages: bash:66","type":"invalid_request_error"}}`

	errorType, statusCode, detail := classifyAPIError(errMsg)

	if errorType != "orphan_tool_call" {
		t.Errorf("errorType = %q, want %q", errorType, "orphan_tool_call")
	}
	if statusCode != "400" {
		t.Errorf("statusCode = %q, want %q", statusCode, "400")
	}
	if detail != "tool_use blocks missing matching tool_result responses" {
		t.Errorf("detail = %q, want specific message", detail)
	}
}

func TestClassifyAPIError_RateLimit(t *testing.T) {
	errMsg := `API error 429: rate_limit_error: too many requests`
	errorType, statusCode, _ := classifyAPIError(errMsg)
	if errorType != "rate_limit" {
		t.Errorf("errorType = %q, want %q", errorType, "rate_limit")
	}
	if statusCode != "429" {
		t.Errorf("statusCode = %q, want %q", statusCode, "429")
	}
}

func TestClassifyAPIError_Overloaded(t *testing.T) {
	errMsg := `API error 529: overloaded_error: the server is overloaded`
	errorType, statusCode, _ := classifyAPIError(errMsg)
	if errorType != "overloaded" {
		t.Errorf("errorType = %q, want %q", errorType, "overloaded")
	}
	if statusCode != "529" {
		t.Errorf("statusCode = %q, want %q", statusCode, "529")
	}
}

func TestClassifyAPIError_Auth(t *testing.T) {
	errMsg := `API error 401: authentication_error: invalid x-api-key`
	errorType, statusCode, _ := classifyAPIError(errMsg)
	if errorType != "auth" {
		t.Errorf("errorType = %q, want %q", errorType, "auth")
	}
	if statusCode != "401" {
		t.Errorf("statusCode = %q, want %q", statusCode, "401")
	}
}

func TestClassifyAPIError_InvalidRequest(t *testing.T) {
	errMsg := `API error 400: {"error":{"type":"invalid_request_error","message":"some other issue"}}`
	errorType, statusCode, _ := classifyAPIError(errMsg)
	if errorType != "invalid_request" {
		t.Errorf("errorType = %q, want %q", errorType, "invalid_request")
	}
	if statusCode != "400" {
		t.Errorf("statusCode = %q, want %q", statusCode, "400")
	}
}

func TestClassifyAPIError_Unknown(t *testing.T) {
	errMsg := `connection refused`
	errorType, statusCode, _ := classifyAPIError(errMsg)
	if errorType != "unknown" {
		t.Errorf("errorType = %q, want %q", errorType, "unknown")
	}
	if statusCode != "0" {
		t.Errorf("statusCode = %q, want %q", statusCode, "0")
	}
}

func TestRecordError_NilOTEL(t *testing.T) {
	r := &Runtime{} // no otel configured
	// Should not panic
	r.recordError(context.Background(), "tool", "execution_failure", "bash", fmt.Errorf("timeout"))
}

func TestRecordError_NilSession(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	inst, err := yotel.NewInstruments(meter)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{
		otel: &OTELConfig{Inst: inst},
		// session is nil
	}
	// Should not panic
	r.recordError(context.Background(), "tool", "execution_failure", "bash", fmt.Errorf("timeout"))
}

func TestRecordError_WithSession(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	inst, err := yotel.NewInstruments(meter)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{
		otel:    &OTELConfig{Inst: inst},
		session: &session.Session{ID: "test-session"},
	}
	// Should not panic, should record metric
	r.recordError(context.Background(), "tool", "execution_failure", "bash", fmt.Errorf("timeout"))
}
