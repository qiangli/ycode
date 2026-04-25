//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func mcpPost(t *testing.T, url string, req jsonrpcRequest) jsonrpcResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	status, respBody := httpPost(t, url, "application/json", string(body))
	if status != http.StatusOK {
		t.Fatalf("MCP POST returned %d, want 200; body: %s", status, respBody)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(respBody), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, respBody)
	}
	return resp
}

func TestMCP(t *testing.T) {
	requireConnectivity(t)
	pulseURL := baseURL(t) + "/pulse/"

	t.Run("Initialize", func(t *testing.T) {
		resp := mcpPost(t, pulseURL, jsonrpcRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "initialize",
			Params: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"clientInfo": map[string]any{
					"name":    "integration-test",
					"version": "1.0.0",
				},
			},
		})
		if resp.Error != nil {
			t.Fatalf("initialize error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
		}
		if resp.Result == nil {
			t.Fatal("initialize returned nil result")
		}

		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
			Capabilities struct {
				Tools *struct{} `json:"tools"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.ProtocolVersion == "" {
			t.Error("protocolVersion is empty")
		}
		if result.ServerInfo.Name == "" {
			t.Error("serverInfo.name is empty")
		}
	})

	t.Run("ToolsList", func(t *testing.T) {
		resp := mcpPost(t, pulseURL, jsonrpcRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/list",
		})
		if resp.Error != nil {
			t.Fatalf("tools/list error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
		}

		var result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("unmarshal tools: %v", err)
		}
		if len(result.Tools) == 0 {
			t.Fatal("tools/list returned empty tools array")
		}

		// Verify expected core tools are present.
		expected := map[string]bool{
			"query_traces":  false,
			"query_logs":    false,
			"query_metrics": false,
		}
		for _, tool := range result.Tools {
			if _, ok := expected[tool.Name]; ok {
				expected[tool.Name] = true
			}
		}
		for name, found := range expected {
			if !found {
				t.Errorf("expected tool %q not found in tools/list", name)
			}
		}
	})

	t.Run("ToolsCallQueryTraces", func(t *testing.T) {
		resp := mcpPost(t, pulseURL, jsonrpcRequest{
			JSONRPC: "2.0",
			ID:      3,
			Method:  "tools/call",
			Params: map[string]any{
				"name": "query_traces",
				"arguments": map[string]any{
					"query_type": "recent_spans",
					"limit":      5,
				},
			},
		})
		if resp.Error != nil {
			t.Fatalf("tools/call error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
		}
		if resp.Result == nil {
			t.Fatal("tools/call returned nil result")
		}

		// MCP tools/call returns {content: [{type, text}]}
		var result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("unmarshal tool result: %v", err)
		}
		if len(result.Content) == 0 {
			t.Error("tool result has no content blocks")
		}
	})

	t.Run("ToolsCallQueryMetrics", func(t *testing.T) {
		resp := mcpPost(t, pulseURL, jsonrpcRequest{
			JSONRPC: "2.0",
			ID:      4,
			Method:  "tools/call",
			Params: map[string]any{
				"name": "query_metrics",
				"arguments": map[string]any{
					"query_type": "tool_stats",
					"limit":      5,
				},
			},
		})
		if resp.Error != nil {
			t.Fatalf("tools/call error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
		}
		if resp.Result == nil {
			t.Fatal("tools/call returned nil result")
		}
	})

	t.Run("MethodNotFound", func(t *testing.T) {
		resp := mcpPost(t, pulseURL, jsonrpcRequest{
			JSONRPC: "2.0",
			ID:      99,
			Method:  "nonexistent/method",
		})
		if resp.Error == nil {
			t.Fatal("expected error for unknown method, got success")
		}
		if resp.Error.Code != -32601 {
			t.Errorf("expected error code -32601 (method not found), got %d", resp.Error.Code)
		}
	})
}
