// MCP capability family D: Ollama proxy. Foreign agents (Claude Code,
// Codex, Aider, Gemini CLI, opencode, ...) reach a locally-running
// Ollama via these MCP tools without their own inference runner.
//
// Same-machine assumption: the Ollama HTTP API is reachable on
// localhost. Base URL precedence: constructor arg > YCODE_OLLAMA_BASE_URL
// > OLLAMA_BASE_URL > http://localhost:11434.

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler implements the Ollama-proxy capability family. It is a
// thin wrapper over Ollama's REST API at /api/tags and /api/chat. No
// state beyond the base URL + HTTP client; safe for concurrent use.
type MCPHandler struct {
	baseURL string
	client  *http.Client
}

// NewMCPHandler returns a handler that proxies to baseURL. Empty
// baseURL resolves via the env-then-default chain documented at the
// top of this file. A 60s per-request timeout is applied — most chat
// completions on a local CPU fit comfortably; long-running streams
// should call /api/chat directly via the HTTP endpoint exposed by
// `ycode serve`.
func NewMCPHandler(baseURL string) *MCPHandler {
	if baseURL == "" {
		if v := os.Getenv("YCODE_OLLAMA_BASE_URL"); v != "" {
			baseURL = v
		} else if v := os.Getenv("OLLAMA_BASE_URL"); v != "" {
			baseURL = v
		} else {
			baseURL = "http://localhost:11434"
		}
	}
	return &MCPHandler{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "ollama_list_models",
			Description: "List models available on the locally-running Ollama instance. Returns the raw " +
				"/api/tags JSON: {models: [{name, size, modified_at, digest, details: {...}}]}. Useful as " +
				"a discovery step before calling ollama_chat with a specific model name.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name: "ollama_chat",
			Description: "Run a chat completion against the locally-running Ollama instance. " +
				"`model` is the Ollama model tag (e.g. llama3.2, qwen2.5-coder:7b). `messages` is an " +
				"array of {role, content} objects (roles: system, user, assistant). Returns Ollama's " +
				"/api/chat JSON envelope verbatim. Non-streaming — stream=false is set internally so the " +
				"full response is collected before returning. For long generations use the HTTP /api " +
				"endpoint exposed by `ycode serve` directly.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"model":    {"type": "string"},
					"messages": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"role":    {"type": "string", "description": "system | user | assistant"},
								"content": {"type": "string"}
							},
							"required": ["role", "content"]
						}
					},
					"options": {"type": "object", "description": "Optional Ollama model options (temperature, top_p, num_ctx, ...). Passed through verbatim."}
				},
				"required": ["model", "messages"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode — both tools are non-mutating reads of an external
// HTTP service. ReadOnly is the right tier; chat completion does not
// touch the filesystem.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "ollama_list_models":
		return h.handleList(ctx)
	case "ollama_chat":
		return h.handleChat(ctx, input)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

func (h *MCPHandler) handleList(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/api/tags", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama unreachable at %s: %w", h.baseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func (h *MCPHandler) handleChat(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Model    string          `json:"model"`
		Messages json.RawMessage `json:"messages"`
		Options  json.RawMessage `json:"options,omitempty"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Model == "" {
		return "", fmt.Errorf("model is required")
	}
	if len(args.Messages) == 0 {
		return "", fmt.Errorf("messages is required")
	}

	payload := map[string]any{
		"model":    args.Model,
		"messages": json.RawMessage(args.Messages),
		"stream":   false,
	}
	if len(args.Options) > 0 {
		payload["options"] = json.RawMessage(args.Options)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama unreachable at %s: %w", h.baseURL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return string(respBody), nil
}
