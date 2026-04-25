package observability

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHTTPHandler serves the MCP protocol over HTTP.
// POST requests carry JSON-RPC bodies, responses are JSON-RPC.
type MCPHTTPHandler struct {
	server *mcp.Server
}

// NewMCPHTTPHandler creates an HTTP handler wrapping an MCP server.
func NewMCPHTTPHandler(handler mcp.ServerHandler) *MCPHTTPHandler {
	return &MCPHTTPHandler{
		server: mcp.NewServer(handler),
	}
}

func (h *MCPHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "MCP server accepts POST requests with JSON-RPC body", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req mcp.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errResp := mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &mcp.JSONRPCError{Code: -32700, Message: "parse error: " + err.Error()},
		}
		json.NewEncoder(w).Encode(errResp)
		return
	}

	resp, err := h.server.HandleRequest(r.Context(), &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		errResp := mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.JSONRPCError{Code: -32603, Message: err.Error()},
		}
		json.NewEncoder(w).Encode(errResp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
