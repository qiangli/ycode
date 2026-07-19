package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The OpenAI Responses API (/v1/responses).
//
// The gpt-5 family is designed for /v1/responses, not /v1/chat/completions: in
// chat/completions gpt-5 REJECTS function tools alongside reasoning_effort
// ("... use /v1/responses or set reasoning_effort to none/minimal"), which would
// strand ycode with no reasoning while codex — which uses /v1/responses — gets
// full reasoning + tools. So for OpenAI gpt-5 models ycode speaks /v1/responses,
// mapping its own Anthropic-shaped Request/StreamEvent onto the Responses wire and
// back so the rest of the runtime is unchanged. Other OpenAI-compatible providers
// (Kimi, DeepSeek, GLM, local) stay on /v1/chat/completions.

// useResponsesAPI reports whether this request should go to /v1/responses.
func (c *OpenAICompatClient) useResponsesAPI(req *Request) bool {
	return c.kind == ProviderOpenAI && isGPT5Family(req.Model)
}

// responsesRequest is the /v1/responses request body.
type responsesRequest struct {
	Model           string              `json:"model"`
	Input           []any               `json:"input"`
	Instructions    string              `json:"instructions,omitempty"`
	Tools           []responsesTool     `json:"tools,omitempty"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Stream          bool                `json:"stream"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
	// Include asks the server to return the encrypted reasoning payload so it can be
	// replayed on the next turn (the codex pattern) — this is what lets gpt-5 keep its
	// chain-of-thought across tool round-trips instead of re-deriving it each time.
	Include []string `json:"include,omitempty"`
	// Store=false keeps the exchange stateless: ycode resends full history each turn
	// (no previous_response_id), so there is nothing to reuse server-side.
	Store bool `json:"store"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// responsesTool is the Responses API tool shape — FLAT (name/description/parameters
// at the top level), unlike chat/completions which nests them under "function".
type responsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// buildResponsesRequest converts ycode's Anthropic-shaped Request into a
// /v1/responses body.
func (c *OpenAICompatClient) buildResponsesRequest(req *Request) *responsesRequest {
	rr := &responsesRequest{
		Model:           req.Model,
		Input:           buildResponsesInput(req),
		Instructions:    req.System,
		MaxOutputTokens: req.MaxTokens,
		Stream:          req.Stream,
		Store:           false,
	}
	// The Responses API DOES accept reasoning alongside tools (that is the whole
	// reason to use it). Map ycode's effort through; the terra/sol/luna variants'
	// baked default applies when it is empty.
	if req.ReasoningEffort != "" && req.ReasoningEffort != "none" {
		rr.Reasoning = &responsesReasoning{Effort: req.ReasoningEffort}
	}
	// Always ask for the encrypted reasoning payload; it is only useful (and only
	// replayed) once reasoning-item carryover is threaded through the session, but
	// requesting it is harmless and keeps the wire ready.
	if rr.Reasoning != nil {
		rr.Include = []string{"reasoning.encrypted_content"}
	}
	for _, tool := range req.Tools {
		rr.Tools = append(rr.Tools, responsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}
	return rr
}

// buildResponsesInput flattens the conversation into Responses input items. A
// message may become several items: text → a role message, a tool_use → a
// function_call item, a tool_result → a function_call_output item. Prior thinking
// blocks are NOT resent (reasoning is server-managed / turn-local).
func buildResponsesInput(req *Request) []any {
	var input []any
	for _, msg := range req.Messages {
		// Collect same-role text so multiple text blocks fold into one message.
		var text strings.Builder
		flushText := func() {
			if text.Len() == 0 {
				return
			}
			input = append(input, map[string]any{
				"role":    string(msg.Role),
				"content": text.String(),
			})
			text.Reset()
		}
		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeText:
				text.WriteString(block.Text)
			case ContentTypeToolUse:
				flushText()
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   block.ID,
					"name":      block.Name,
					"arguments": string(block.Input),
				})
			case ContentTypeToolResult:
				flushText()
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": block.ToolUseID,
					"output":  block.Content,
				})
			case ContentTypeThinking:
				// Reasoning is turn-local; do not resend as input.
			}
		}
		flushText()
	}
	return input
}

// sendResponses drives the /v1/responses endpoint, mirroring Send's HTTP retry +
// content-encoding plumbing. It owns the events/errc channels (closes both).
func (c *OpenAICompatClient) sendResponses(ctx context.Context, req *Request, events chan<- *StreamEvent, errc chan<- error) {
	defer close(events)
	defer close(errc)

	body := c.buildResponsesRequest(req)
	data, err := json.Marshal(body)
	if err != nil {
		errc <- fmt.Errorf("marshal responses request: %w", err)
		return
	}

	makeReq := func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/responses", bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		return httpReq, nil
	}

	resp, err := doWithRetry(ctx, c.httpClient, makeReq)
	if err != nil {
		errc <- err
		return
	}
	defer resp.Body.Close()

	bodyReader, err := DecodeResponseBody(resp)
	if err != nil {
		errc <- fmt.Errorf("decode response: %w", err)
		return
	}
	if bodyReader != resp.Body {
		defer bodyReader.Close()
	}

	c.readResponsesStream(bodyReader, events, errc)
}

// responsesEvent is a single /v1/responses SSE event. Every event carries its own
// `type`, so we switch on that rather than the SSE `event:` line.
type responsesEvent struct {
	Type string `json:"type"`
	// output_text.delta / reasoning*.delta / function_call_arguments.delta
	Delta string `json:"delta"`
	// output_item.done carries the finished item.
	Item struct {
		Type      string `json:"type"` // "function_call" | "message" | "reasoning"
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	// completed/incomplete carry the final response with usage.
	Response struct {
		Status string `json:"status"`
		Usage  struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	} `json:"response"`
	// error events carry a message.
	Message string `json:"message"`
}

// readResponsesStream parses /v1/responses SSE and emits ycode's Anthropic-shaped
// StreamEvents — the SAME shapes readStream produces, so the runtime is unchanged.
func (c *OpenAICompatClient) readResponsesStream(body io.Reader, events chan<- *StreamEvent, errc chan<- error) {
	parser := NewSSEParser(body)
	sawToolCall := false

	for {
		raw, err := parser.Next()
		if err != nil {
			if err == io.EOF {
				events <- &StreamEvent{Type: "message_stop"}
			} else {
				errc <- err
			}
			return
		}
		if raw.Data == "" || raw.Data == "[DONE]" {
			continue
		}

		var ev responsesEvent
		if err := json.Unmarshal([]byte(raw.Data), &ev); err != nil {
			continue // a malformed keep-alive is not fatal
		}

		switch ev.Type {
		case "response.output_text.delta":
			delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": ev.Delta})
			events <- &StreamEvent{Type: "content_block_delta", Delta: delta}

		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			delta, _ := json.Marshal(map[string]string{"type": "thinking_delta", "thinking": ev.Delta})
			events <- &StreamEvent{Type: "content_block_delta", Delta: delta}

		case "response.output_item.done":
			if ev.Item.Type == "function_call" {
				sawToolCall = true
				events <- &StreamEvent{
					Type: "content_block_start",
					ContentBlock: &ContentBlock{
						Type:  ContentTypeToolUse,
						ID:    ev.Item.CallID,
						Name:  ev.Item.Name,
						Input: json.RawMessage(ev.Item.Arguments),
					},
				}
				events <- &StreamEvent{Type: "content_block_stop"}
			}

		case "response.completed", "response.incomplete":
			// Usage arrives only at the end (like chat/completions with include_usage).
			in := ev.Response.Usage.InputTokens
			out := ev.Response.Usage.OutputTokens
			cached := ev.Response.Usage.InputTokensDetails.CachedTokens
			events <- &StreamEvent{
				Type:    "message_start",
				Message: &Response{Usage: Usage{InputTokens: in, CacheReadInput: cached}},
			}
			stop := StopReasonEndTurn
			if sawToolCall {
				stop = StopReasonToolUse
			}
			d, _ := json.Marshal(map[string]string{"stop_reason": stop})
			events <- &StreamEvent{Type: "message_delta", Delta: d, Usage: &Usage{OutputTokens: out}}
			events <- &StreamEvent{Type: "message_stop"}
			return

		case "response.failed", "error", "response.error":
			msg := ev.Message
			if msg == "" {
				msg = raw.Data
			}
			errc <- fmt.Errorf("responses API error: %s", msg)
			return
		}
	}
}
