package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// ServerHandler handles incoming MCP requests when ycode acts as an MCP server.
type ServerHandler interface {
	HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error)
	ListTools() []Tool
	ListResources() []Resource
	ReadResource(ctx context.Context, uri string) (string, error)
}

// Server implements the MCP server protocol for ycode.
type Server struct {
	handler ServerHandler
	logger  *slog.Logger

	clientMu   sync.RWMutex
	clientName string // populated by the initialize handshake
}

// NewServer creates a new MCP server.
func NewServer(handler ServerHandler) *Server {
	return &Server{
		handler: handler,
		logger:  slog.Default(),
	}
}

// ClientName returns the connected MCP client's reported name (from
// the initialize handshake's `clientInfo.name`). Empty before any
// initialize call, or when the client didn't supply clientInfo.
func (s *Server) ClientName() string {
	s.clientMu.RLock()
	defer s.clientMu.RUnlock()
	return s.clientName
}

// ctxKey is the unexported type used for context.Value identity.
type ctxKey int

const agentClientKey ctxKey = iota

// WithAgentClient stashes the connected MCP client's name on ctx.
// Downstream observers (e.g. tool middleware) read it via AgentClient.
func WithAgentClient(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, agentClientKey, name)
}

// AgentClient returns the MCP client name previously stashed via
// WithAgentClient. Empty when no MCP server is in the chain.
func AgentClient(ctx context.Context) string {
	if v, ok := ctx.Value(agentClientKey).(string); ok {
		return v
	}
	return ""
}

// HandleRequest processes an incoming JSON-RPC request.
func (s *Server) HandleRequest(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		// Capture clientInfo.name so subsequent tool calls can be
		// attributed back to the foreign agent (Claude Code, Cursor,
		// Codex, etc.). Best-effort: missing or malformed clientInfo
		// is silently tolerated.
		if raw, err := json.Marshal(req.Params); err == nil {
			var params struct {
				ClientInfo struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"clientInfo"`
			}
			if json.Unmarshal(raw, &params) == nil && params.ClientInfo.Name != "" {
				s.clientMu.Lock()
				s.clientName = params.ClientInfo.Name
				s.clientMu.Unlock()
				s.logger.Info("mcp: client connected", "client", params.ClientInfo.Name, "version", params.ClientInfo.Version)
			}
		}
		result := map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "ycode",
				"version": "0.1.0",
			},
		}
		data, _ := json.Marshal(result)
		resp.Result = data

	case "tools/list":
		tools := s.handler.ListTools()
		// MCP clients (Claude Code, Cursor) require an array — a nil
		// slice marshals to JSON null and triggers a Zod validation
		// error on the client. Force an empty array when there are no
		// tools.
		if tools == nil {
			tools = []Tool{}
		}
		result := map[string]any{"tools": tools}
		data, _ := json.Marshal(result)
		resp.Result = data

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if raw, err := json.Marshal(req.Params); err == nil {
			_ = json.Unmarshal(raw, &params)
		}
		// Inject the connected client name (if any) into the call
		// context so the tool middleware can attach it as a span /
		// metric attribute.
		ctx = WithAgentClient(ctx, s.ClientName())
		output, err := s.handler.HandleToolCall(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
		} else {
			result := map[string]any{
				"content": []map[string]string{
					{"type": "text", "text": output},
				},
			}
			data, _ := json.Marshal(result)
			resp.Result = data
		}

	case "resources/list":
		resources := s.handler.ListResources()
		// Same nil-slice guard as tools/list — Claude Code's MCP
		// client logs `"Failed to fetch resources: expected array,
		// received null"` and skips the server otherwise.
		if resources == nil {
			resources = []Resource{}
		}
		result := map[string]any{"resources": resources}
		data, _ := json.Marshal(result)
		resp.Result = data

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if raw, err := json.Marshal(req.Params); err == nil {
			_ = json.Unmarshal(raw, &params)
		}
		content, err := s.handler.ReadResource(ctx, params.URI)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
		} else {
			result := map[string]any{
				"contents": []map[string]string{
					{"uri": params.URI, "text": content},
				},
			}
			data, _ := json.Marshal(result)
			resp.Result = data
		}

	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("method not found: %s", req.Method),
		}
	}

	return resp, nil
}
