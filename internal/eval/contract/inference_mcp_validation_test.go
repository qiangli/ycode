// Phase-2 contract for the inference (Ollama proxy) MCP family. Hermetic
// — uses httptest to stand up a fake Ollama returning canned JSON. The
// handler short-circuits env reads when constructed with a non-empty
// baseURL, so this test does not depend on a real Ollama or any env state.
package contract

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/inference"
)

func newFakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2","size":1000,"modified_at":"2026-01-01T00:00:00Z"}]}`))
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":false`) {
			t.Errorf("expected stream=false in chat payload, got: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"llama3.2","message":{"role":"assistant","content":"hello back"},"done":true}`))
	})
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"nomic-embed-text"`) {
			t.Errorf("expected model in embed payload, got: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"nomic-embed-text","embeddings":[[0.1,0.2,0.3]]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestInferenceMCP_ToolsListExposesAllThree(t *testing.T) {
	t.Parallel()
	fake := newFakeOllama(t)
	srv := buildPhase0Server(inference.NewMCPHandler(fake.URL))

	resp := mustReq(t, srv, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	body := string(resp.Result)
	for _, name := range []string{"ollama_list_models", "ollama_chat", "ollama_embed"} {
		if !strings.Contains(body, `"`+name+`"`) {
			t.Fatalf("tools/list missing %s: %s", name, body)
		}
	}
}

func TestInferenceMCP_ListModelsRoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeOllama(t)
	srv := buildPhase0Server(inference.NewMCPHandler(fake.URL))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name":      "ollama_list_models",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("ollama_list_models error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `\"llama3.2\"`) {
		t.Fatalf("ollama_list_models result missing llama3.2: %s", resp.Result)
	}
}

func TestInferenceMCP_ChatRoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeOllama(t)
	srv := buildPhase0Server(inference.NewMCPHandler(fake.URL))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "ollama_chat",
		"arguments": map[string]any{
			"model": "llama3.2",
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("ollama_chat error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `hello back`) {
		t.Fatalf("ollama_chat result missing response: %s", resp.Result)
	}
}

func TestInferenceMCP_EmbedRoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeOllama(t)
	srv := buildPhase0Server(inference.NewMCPHandler(fake.URL))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name": "ollama_embed",
		"arguments": map[string]any{
			"model": "nomic-embed-text",
			"input": "some text to embed",
		},
	})
	if resp.Error != nil {
		t.Fatalf("ollama_embed error: %v", resp.Error)
	}
	if !strings.Contains(string(resp.Result), `embeddings`) {
		t.Fatalf("ollama_embed result missing embeddings field: %s", resp.Result)
	}
}

func TestInferenceMCP_EmbedRequiresModelAndInput(t *testing.T) {
	t.Parallel()
	fake := newFakeOllama(t)
	srv := buildPhase0Server(inference.NewMCPHandler(fake.URL))

	resp := mustReq(t, srv, "tools/call", map[string]any{
		"name":      "ollama_embed",
		"arguments": map[string]any{"input": "x"},
	})
	if resp.Error == nil {
		t.Fatalf("ollama_embed without model should fail")
	}
	if !strings.Contains(resp.Error.Message, "model is required") {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = mustReq(t, srv, "tools/call", map[string]any{
		"name":      "ollama_embed",
		"arguments": map[string]any{"model": "x"},
	})
	if resp.Error == nil {
		t.Fatalf("ollama_embed without input should fail")
	}
	if !strings.Contains(resp.Error.Message, "input is required") {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}
