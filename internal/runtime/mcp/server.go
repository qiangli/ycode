package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
}

// NewServer creates a new MCP server.
func NewServer(handler ServerHandler) *Server {
	return &Server{
		handler: handler,
		logger:  slog.Default(),
	}
}

// HandleRequest processes an incoming JSON-RPC request.
func (s *Server) HandleRequest(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
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
