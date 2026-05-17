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

// Content is one MCP tool-result content block (text, image, or
// resource). The MCP spec lets a single tools/call response carry an
// ordered array of these so a handler can mix narration with binary
// payloads — e.g. screenshot → [image block + text metadata].
//
// Field set is the union of the spec's TextContent and ImageContent
// shapes; unused fields drop out via omitempty. Use ContentText /
// ContentImage helpers rather than building literals so the type
// discriminator stays correct.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // base64 for image/audio
	MimeType string `json:"mimeType,omitempty"` // required for image/audio
}

// ContentText returns a text content block.
func ContentText(text string) Content { return Content{Type: "text", Text: text} }

// ContentImage returns an image content block. `data` must already be
// base64-encoded; `mimeType` is the IANA type ("image/png",
// "image/jpeg", …).
func ContentImage(data, mimeType string) Content {
	return Content{Type: "image", Data: data, MimeType: mimeType}
}

// RichHandler is an optional interface a ServerHandler can implement to
// return structured tool-result content blocks instead of a single
// stringified payload. Handlers that don't implement it stay on the
// text-only path — the server auto-wraps their string in one text
// block — so every existing handler keeps working unchanged.
//
// The motivating case is browser_screenshot: returning the PNG via an
// image content block lets foreign agents (Claude Code, Cursor, …)
// render it inline without the consumer side base64-decoding a JSON
// envelope by hand.
type RichHandler interface {
	HandleToolCallRich(ctx context.Context, name string, input json.RawMessage) ([]Content, error)
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
		content, err := dispatchToolCall(ctx, s.handler, params.Name, params.Arguments)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
		} else {
			result := map[string]any{"content": content}
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

// dispatchToolCall routes a tools/call through the rich content path
// when the handler supports it, falling back to the legacy
// HandleToolCall (string) path otherwise. Both CompositeHandler and
// GatedHandler implement RichHandler conditionally, so a screenshot
// going through composite → gated → browsermcp still surfaces an
// image content block on the wire.
func dispatchToolCall(ctx context.Context, h ServerHandler, name string, args json.RawMessage) ([]Content, error) {
	if rich, ok := h.(RichHandler); ok {
		return rich.HandleToolCallRich(ctx, name, args)
	}
	out, err := h.HandleToolCall(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return []Content{ContentText(out)}, nil
}
