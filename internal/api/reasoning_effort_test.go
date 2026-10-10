package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureBody records the exact JSON body a provider client put on the wire.
// Sprint 412 Story #896: declared reasoning effort and declared cache breaks
// are only real once they appear in that body, so every assertion here reads
// the request a test HTTP server received rather than an intermediate struct.
type captureBody struct {
	path string
	body []byte
}

func captureServer(t *testing.T, sink *captureBody) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader := io.Reader(r.Body)
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("gzip request body: %v", err)
				return
			}
			defer gz.Close()
			reader = gz
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		sink.path, sink.body = r.URL.Path, data
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	t.Cleanup(server.Close)
	return server
}

// drainStream consumes a provider stream so the sending goroutine finishes
// before the test inspects what it sent.
func drainStream(events <-chan *StreamEvent, errs <-chan error) {
	for events != nil || errs != nil {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		}
	}
}

func decodeBody(t *testing.T, data []byte) map[string]any {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("no request body captured")
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode request body: %v (%s)", err, data)
	}
	return body
}

// The gpt-5 family speaks /v1/responses, where reasoning effort is a nested
// reasoning.effort object. A declared effort must arrive there even though the
// request also carries the single model-visible tool.
func TestResponsesRequestCarriesReasoningEffort(t *testing.T) {
	var sink captureBody
	server := captureServer(t, &sink)
	client := NewOpenAICompatClient("k", server.URL+"/v1")
	request := &Request{
		Model:           "gpt-5.6",
		MaxTokens:       256,
		System:          "be brief",
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
		Tools:           []ToolDefinition{{Name: "bashy", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ReasoningEffort: "xhigh",
		Stream:          true,
	}
	drainStream(client.Send(context.Background(), request))

	if sink.path != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", sink.path)
	}
	body := decodeBody(t, sink.body)
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("request has no reasoning object: %s", sink.body)
	}
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("reasoning.effort = %v, want xhigh: %s", reasoning["effort"], sink.body)
	}
}

// Chat completions carries effort as the flat reasoning_effort field, for any
// model that declares one — including a request that offers tools.
func TestChatCompletionsCarriesReasoningEffortWithTools(t *testing.T) {
	var sink captureBody
	server := captureServer(t, &sink)
	client := NewOpenAICompatClient("k", server.URL+"/v1")
	request := &Request{
		Model:           "glm-5.3",
		MaxTokens:       256,
		System:          "be brief",
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
		Tools:           []ToolDefinition{{Name: "bashy", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ReasoningEffort: "high",
		Stream:          true,
	}
	drainStream(client.Send(context.Background(), request))

	if sink.path != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", sink.path)
	}
	body := decodeBody(t, sink.body)
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v, want high: %s", body["reasoning_effort"], sink.body)
	}
}

// A gpt-5-named model reaching chat/completions is not OpenAI's gpt-5 (those
// are routed to /v1/responses by useResponsesAPI), so its declared effort must
// not be dropped either.
func TestChatCompletionsKeepsEffortForGPT5NamedModelWithTools(t *testing.T) {
	client := NewOpenAICompatClient("k", "http://x/v1")
	wire := client.buildRequest(&Request{
		Model:           "gpt-5.6",
		MaxTokens:       256,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
		Tools:           []ToolDefinition{{Name: "bashy", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ReasoningEffort: "high",
	})
	if wire.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", wire.ReasoningEffort)
	}
}

// "none" stays the one effort that disables thinking instead of selecting a
// depth (the Kimi K2.5 shape), and never reaches reasoning.effort.
func TestChatCompletionsDisablesThinkingForEffortNone(t *testing.T) {
	client := NewOpenAICompatClient("k", "http://x/v1")
	wire := client.buildRequest(&Request{
		Model:           "kimi-k2.5",
		MaxTokens:       256,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hi"}}}},
		ReasoningEffort: "none",
	})
	if wire.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort = %q, want empty for none", wire.ReasoningEffort)
	}
	if wire.Thinking == nil || wire.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %#v, want disabled", wire.Thinking)
	}
}
