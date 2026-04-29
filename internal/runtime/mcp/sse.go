package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// SSETransport communicates with an MCP server over HTTP using
// Server-Sent Events for responses.
type SSETransport struct {
	baseURL    string
	httpClient *http.Client

	mu     sync.Mutex
	nextID atomic.Int64
}

// NewSSETransport creates an SSE transport for the given MCP server URL.
func NewSSETransport(baseURL string) *SSETransport {
	return &SSETransport{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// Start is a no-op for SSE transport (no process to spawn).
func (t *SSETransport) Start() error {
	return nil
}

// Call sends a JSON-RPC request via HTTP POST and reads the SSE response.
func (t *SSETransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")

	// If the server responds with JSON directly (not SSE), parse it.
	if strings.HasPrefix(contentType, "application/json") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read JSON response: %w", err)
		}
		var rpcResp JSONRPCResponse
		if err := json.Unmarshal(body, &rpcResp); err != nil {
			return nil, fmt.Errorf("parse JSON-RPC response: %w", err)
		}
		if rpcResp.Error != nil {
			return nil, rpcResp.Error
		}
		return rpcResp.Result, nil
	}

	// Parse SSE stream for the response event.
	return t.readSSEResponse(resp.Body, id)
}

// readSSEResponse reads SSE events until it finds the response matching the given ID.
func (t *SSETransport) readSSEResponse(body io.Reader, id int64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	var eventData strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			eventData.WriteString(strings.TrimPrefix(line, "data: "))
			continue
		}

		// Empty line = end of event.
		if line == "" && eventData.Len() > 0 {
			var rpcResp JSONRPCResponse
			if err := json.Unmarshal([]byte(eventData.String()), &rpcResp); err != nil {
				eventData.Reset()
				continue // skip non-JSON events
			}
			eventData.Reset()

			if rpcResp.ID == id {
				if rpcResp.Error != nil {
					return nil, rpcResp.Error
				}
				return rpcResp.Result, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read SSE stream: %w", err)
	}

	return nil, fmt.Errorf("SSE stream ended without response for request %d", id)
}

// Close is a no-op for SSE transport.
func (t *SSETransport) Close() error {
	return nil
}
