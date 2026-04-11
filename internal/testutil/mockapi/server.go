package mockapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/api"
)

// Response is a preconfigured response for the mock server.
type Response struct {
	Content    string
	StopReason string
	ToolUse    *api.ContentBlock
}

// Server is a mock Anthropic API server for testing.
type Server struct {
	server    *httptest.Server
	mu        sync.Mutex
	responses []Response
	requests  []*api.Request
	callCount int
}

// NewServer creates a mock Anthropic API server.
func NewServer() *Server {
	s := &Server{}
	s.server = httptest.NewServer(http.HandlerFunc(s.handleMessages))
	return s
}

// URL returns the server URL.
func (s *Server) URL() string {
	return s.server.URL
}

// Close shuts down the server.
func (s *Server) Close() {
	s.server.Close()
}

// AddResponse enqueues a response.
func (s *Server) AddResponse(resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, resp)
}

// AddTextResponse enqueues a simple text response.
func (s *Server) AddTextResponse(text string) {
	s.AddResponse(Response{Content: text, StopReason: api.StopReasonEndTurn})
}

// Requests returns all received requests.
func (s *Server) Requests() []*api.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*api.Request{}, s.requests...)
}

// CallCount returns the number of requests received.
func (s *Server) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req api.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, &req)
	s.callCount++

	var resp Response
	if len(s.responses) > 0 {
		resp = s.responses[0]
		s.responses = s.responses[1:]
	} else {
		resp = Response{Content: "Mock response", StopReason: api.StopReasonEndTurn}
	}
	s.mu.Unlock()

	if req.Stream {
		s.handleStreaming(w, &resp)
	} else {
		s.handleNonStreaming(w, &resp)
	}
}

func (s *Server) handleStreaming(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send message_start with usage.
	msgStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    "msg_mock",
			"type":  "message",
			"role":  "assistant",
			"model": "mock-model",
			"usage": map[string]int{
				"input_tokens":  100,
				"output_tokens": 0,
			},
		},
	}
	writeSSE(w, "message_start", msgStart)
	flusher.Flush()

	// Send content_block_start.
	writeSSE(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]string{"type": "text", "text": ""},
	})
	flusher.Flush()

	// Send content in chunks.
	for i, chunk := range splitIntoChunks(resp.Content, 20) {
		_ = i
		writeSSE(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]string{"type": "text_delta", "text": chunk},
		})
		flusher.Flush()
	}

	// Send content_block_stop.
	writeSSE(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	flusher.Flush()

	// Send message_delta with usage.
	outputTokens := len(resp.Content) / 4
	if outputTokens < 5 {
		outputTokens = 5
	}
	writeSSE(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]string{
			"stop_reason": resp.StopReason,
		},
		"usage": map[string]int{
			"output_tokens": outputTokens,
		},
	})
	flusher.Flush()

	// Send message_stop.
	writeSSE(w, "message_stop", map[string]any{"type": "message_stop"})
	flusher.Flush()
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, resp *Response) {
	content := []api.ContentBlock{{Type: api.ContentTypeText, Text: resp.Content}}
	if resp.ToolUse != nil {
		content = append(content, *resp.ToolUse)
	}

	apiResp := api.Response{
		ID:         "msg_mock",
		Type:       "message",
		Role:       api.RoleAssistant,
		Content:    content,
		Model:      "mock-model",
		StopReason: resp.StopReason,
		Usage:      api.Usage{InputTokens: 10, OutputTokens: 20},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiResp)
}

func writeSSE(w http.ResponseWriter, event string, data any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
}

func splitIntoChunks(s string, chunkSize int) []string {
	if len(s) == 0 {
		return []string{""}
	}
	var chunks []string
	for len(s) > 0 {
		end := chunkSize
		if end > len(s) {
			end = len(s)
		}
		// Don't split in the middle of a word.
		if end < len(s) {
			if idx := strings.LastIndex(s[:end], " "); idx > 0 {
				end = idx + 1
			}
		}
		chunks = append(chunks, s[:end])
		s = s[end:]
	}
	return chunks
}
