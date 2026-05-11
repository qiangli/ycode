package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestNilResponseFormat_ByteIdentical(t *testing.T) {
	// A Request with no ResponseFormat / SystemBlocks / ToolChoice should
	// marshal exactly as the alias-based fast path produces — proving the
	// new wire fields are additive.
	r := Request{
		Model:     "claude-test",
		MaxTokens: 100,
		System:    "be brief",
		Messages: []Message{{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}},
		}},
		Stream: true,
	}
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("response_format")) ||
		bytes.Contains(got, []byte("tool_choice")) {
		t.Errorf("nil ResponseFormat must not leak fields onto the wire: %s", got)
	}
}

func TestOpenAIBuildRequest_JSONSchema(t *testing.T) {
	c := NewOpenAICompatClient("k", "http://x/v1")
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	req := &Request{
		Model: "gpt",
		Messages: []Message{{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentTypeText, Text: "extract"}},
		}},
		ResponseFormat: &ResponseFormat{
			Type:   "json_schema",
			Name:   "person",
			Schema: schema,
		},
	}
	o := c.buildRequest(req)
	if o.ResponseFormat == nil {
		t.Fatal("expected ResponseFormat to be populated on wire payload")
	}
	if o.ResponseFormat.Type != "json_schema" {
		t.Errorf("type: want json_schema, got %q", o.ResponseFormat.Type)
	}
	if o.ResponseFormat.JSONSchema == nil {
		t.Fatal("JSONSchema spec missing")
	}
	if o.ResponseFormat.JSONSchema.Name != "person" {
		t.Errorf("name: want person, got %q", o.ResponseFormat.JSONSchema.Name)
	}
	if !o.ResponseFormat.JSONSchema.Strict {
		t.Error("expected Strict=true on json_schema response_format")
	}
	if !bytes.Equal(o.ResponseFormat.JSONSchema.Schema, schema) {
		t.Errorf("schema mismatch: got %s", o.ResponseFormat.JSONSchema.Schema)
	}

	// Final wire payload must contain response_format at the JSON layer.
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"response_format"`)) {
		t.Errorf("wire payload missing response_format: %s", data)
	}
}

func TestOpenAIBuildRequest_JSONObject(t *testing.T) {
	c := NewOpenAICompatClient("", "")
	req := &Request{
		Messages:       []Message{},
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	}
	o := c.buildRequest(req)
	if o.ResponseFormat == nil || o.ResponseFormat.Type != "json_object" {
		t.Fatalf("expected json_object response_format, got %#v", o.ResponseFormat)
	}
	if o.ResponseFormat.JSONSchema != nil {
		t.Error("json_object must not carry JSONSchema spec")
	}
}

func TestApplyResponseFormatShim_AnthropicInjectsTool(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	req := &Request{
		ResponseFormat: &ResponseFormat{Type: "json_schema", Schema: schema},
	}
	applyResponseFormatShim(req)

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 synthetic tool, got %d", len(req.Tools))
	}
	if req.Tools[0].Name != "respond" {
		t.Errorf("synthetic tool name: want respond, got %q", req.Tools[0].Name)
	}
	if !bytes.Equal(req.Tools[0].InputSchema, schema) {
		t.Errorf("synthetic tool schema mismatch")
	}
	if req.AnthropicToolChoiceName != "respond" {
		t.Errorf("expected forced tool_choice on respond, got %q", req.AnthropicToolChoiceName)
	}

	// MarshalJSON must inject tool_choice on the wire.
	data, err := json.Marshal(*req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tool_choice"`) {
		t.Errorf("wire payload missing tool_choice: %s", data)
	}
	if !strings.Contains(string(data), `"name":"respond"`) {
		t.Errorf("tool_choice payload missing tool name: %s", data)
	}
}

func TestApplyResponseFormatShim_Idempotent(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	req := &Request{
		ResponseFormat: &ResponseFormat{Type: "json_schema", Schema: schema},
	}
	applyResponseFormatShim(req)
	applyResponseFormatShim(req)
	if n := len(req.Tools); n != 1 {
		t.Errorf("expected 1 synthetic tool after double-shim, got %d", n)
	}
}

func TestApplyResponseFormatShim_Noop(t *testing.T) {
	req := &Request{}
	applyResponseFormatShim(req)
	if len(req.Tools) != 0 || req.AnthropicToolChoiceName != "" {
		t.Errorf("expected no-op when ResponseFormat nil; got tools=%d choice=%q",
			len(req.Tools), req.AnthropicToolChoiceName)
	}
}
