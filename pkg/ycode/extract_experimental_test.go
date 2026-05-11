//go:build experimental

package ycode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

func TestExtract_OpenAICompatPath(t *testing.T) {
	canned := `{"email":"a@b.c"}`
	stub := newStubProvider(api.ProviderOpenAI)
	stub.streamFunc = func(req *api.Request) []*api.StreamEvent {
		// Verify the wire layer received the schema as response_format.
		if req.ResponseFormat == nil {
			t.Error("Extract: expected ResponseFormat to be set on Request")
		} else if req.ResponseFormat.Type != "json_schema" {
			t.Errorf("Extract: expected json_schema, got %q", req.ResponseFormat.Type)
		}
		if req.Stream {
			t.Error("Extract: expected Stream=false on OpenAI-compat path")
		}
		// Emit a single text_delta carrying the JSON output.
		delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": canned})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: delta}}
	}

	a, err := NewAgent(WithProvider(stub), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	schema := json.RawMessage(`{"type":"object","properties":{"email":{"type":"string"}}}`)
	out, err := a.Extract(context.Background(), "find email", ExtractOptions{Schema: schema})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if string(out) != canned {
		t.Errorf("Extract output mismatch: want %s, got %s", canned, out)
	}
}

func TestExtract_AnthropicShimPath(t *testing.T) {
	// Anthropic shim translates ResponseFormat into a forced tool_use; the
	// provider emits the JSON via input_json_delta blocks instead of
	// text_delta. Extract must transparently handle either shape.
	canned := `{"score":7}`
	stub := newStubProvider(api.ProviderAnthropic)
	stub.streamFunc = func(req *api.Request) []*api.StreamEvent {
		if req.ResponseFormat == nil {
			t.Error("expected ResponseFormat set on Request")
		}
		// Emit incremental input_json_delta chunks like Anthropic does.
		ev1, _ := json.Marshal(map[string]string{"type": "input_json_delta", "partial_json": canned[:5]})
		ev2, _ := json.Marshal(map[string]string{"type": "input_json_delta", "partial_json": canned[5:]})
		return []*api.StreamEvent{
			{Type: "content_block_delta", Delta: ev1},
			{Type: "content_block_delta", Delta: ev2},
		}
	}

	a, err := NewAgent(WithProvider(stub), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	schema := json.RawMessage(`{"type":"object","properties":{"score":{"type":"integer"}}}`)
	out, err := a.Extract(context.Background(), "score this", ExtractOptions{
		Schema:     schema,
		SchemaName: "rate",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if string(out) != canned {
		t.Errorf("Extract output mismatch: want %s, got %s", canned, out)
	}
}

func TestExtract_EmptyResponseIsError(t *testing.T) {
	stub := newStubProvider(api.ProviderOpenAI)
	stub.streamFunc = func(*api.Request) []*api.StreamEvent { return nil }

	a, err := NewAgent(WithProvider(stub), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	_, err = a.Extract(context.Background(), "x", ExtractOptions{})
	if err == nil {
		t.Error("expected error when provider returns no content")
	}
}

func TestAgentProviderEscapeHatch(t *testing.T) {
	stub := newStubProvider(api.ProviderOpenAI)
	a, err := NewAgent(WithProvider(stub), WithoutBuiltinTools())
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if a.Provider() == nil {
		t.Fatal("Provider() returned nil")
	}
	if a.Provider().Kind() != api.ProviderOpenAI {
		t.Errorf("Provider kind: want openai, got %s", a.Provider().Kind())
	}
}
