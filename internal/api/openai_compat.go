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

// OpenAICompatClient implements Provider for OpenAI-compatible APIs
// (OpenAI, xAI/Grok, DashScope/Qwen, Moonshot/Kimi, etc.).
type OpenAICompatClient struct {
	apiKey  string
	baseURL string
	kind    ProviderKind
}

// NewOpenAICompatClient creates an OpenAI-compatible client.
func NewOpenAICompatClient(apiKey, baseURL string) *OpenAICompatClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAICompatClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		kind:    ProviderOpenAI,
	}
}

// Kind returns the provider type.
func (c *OpenAICompatClient) Kind() ProviderKind {
	return c.kind
}

// Send sends a request and returns a channel of stream events.
func (c *OpenAICompatClient) Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
	events := make(chan *StreamEvent, 64)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		body := c.buildRequest(req)
		data, err := json.Marshal(body)
		if err != nil {
			errc <- fmt.Errorf("marshal request: %w", err)
			return
		}

		// Compress request body if beneficial.
		compressedData, contentEncoding := CompressGzip(data)

		makeReq := func() (*http.Request, error) {
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
				c.baseURL+"/chat/completions", bytes.NewReader(compressedData))
			if err != nil {
				return nil, fmt.Errorf("create request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")
			if contentEncoding != "" {
				httpReq.Header.Set("Content-Encoding", contentEncoding)
			}
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
			return httpReq, nil
		}

		resp, err := doWithRetry(ctx, http.DefaultClient, makeReq)
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()

		if req.Stream {
			c.readStream(resp.Body, events, errc)
		} else {
			c.readNonStream(resp.Body, events, errc)
		}
	}()

	return events, errc
}

// openaiRequest is the OpenAI chat completion request format.
type openaiRequest struct {
	Model         string               `json:"model"`
	Messages      []openaiMessage      `json:"messages"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	Stream        bool                 `json:"stream"`
	Tools         []openaiTool         `json:"tools,omitempty"`
	StreamOptions *openaiStreamOptions `json:"stream_options,omitempty"`
}

// openaiStreamOptions enables usage reporting in streaming mode.
type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openaiMessage struct {
	Role             string           `json:"role"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

func (c *OpenAICompatClient) buildRequest(req *Request) *openaiRequest {
	oReq := &openaiRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}

	// Enable usage reporting in streaming mode.
	if req.Stream {
		oReq.StreamOptions = &openaiStreamOptions{IncludeUsage: true}
	}

	// Add system message if present.
	if req.System != "" {
		oReq.Messages = append(oReq.Messages, openaiMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert messages, handling tool_use and tool_result blocks.
	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleAssistant:
			om := openaiMessage{Role: "assistant"}
			for _, block := range msg.Content {
				switch block.Type {
				case ContentTypeText:
					om.Content += block.Text
				case ContentTypeThinking:
					om.ReasoningContent += block.Text
				case ContentTypeToolUse:
					om.ToolCalls = append(om.ToolCalls, openaiToolCall{
						ID:   block.ID,
						Type: "function",
						Function: openaiFunction{
							Name:      block.Name,
							Arguments: string(block.Input),
						},
					})
				}
			}
			oReq.Messages = append(oReq.Messages, om)
		case RoleUser:
			// Check if this is a tool_result message.
			hasToolResults := false
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult {
					hasToolResults = true
					break
				}
			}
			if hasToolResults {
				// Each tool_result becomes a separate "tool" role message in OpenAI format.
				for _, block := range msg.Content {
					if block.Type == ContentTypeToolResult {
						oReq.Messages = append(oReq.Messages, openaiMessage{
							Role:       "tool",
							Content:    block.Content,
							ToolCallID: block.ToolUseID,
						})
					}
				}
			} else {
				// Regular user message.
				text := ""
				for _, block := range msg.Content {
					if block.Type == ContentTypeText {
						text += block.Text
					}
				}
				oReq.Messages = append(oReq.Messages, openaiMessage{
					Role:    "user",
					Content: text,
				})
			}
		}
	}

	// Convert tools.
	for _, tool := range req.Tools {
		oReq.Tools = append(oReq.Tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	return oReq
}

// openaiStreamDelta represents the delta in a streaming chunk.
type openaiStreamDelta struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func (c *OpenAICompatClient) readStream(body io.Reader, events chan<- *StreamEvent, errc chan<- error) {
	parser := NewSSEParser(body)

	// Track active tool calls by index for incremental argument building.
	type toolCallState struct {
		id   string
		name string
		args strings.Builder
	}
	activeTools := make(map[int]*toolCallState)

	for {
		raw, err := parser.Next()
		if err != nil {
			if err == io.EOF {
				// Finalize any active tool calls.
				for idx, tc := range activeTools {
					// Emit content_block_stop for each tool.
					events <- &StreamEvent{
						Type:  "content_block_stop",
						Index: idx,
					}
					_ = tc // already emitted start
				}
				if len(activeTools) > 0 {
					// Signal stop_reason = tool_use.
					delta, _ := json.Marshal(map[string]string{"stop_reason": "tool_use"})
					events <- &StreamEvent{Type: "message_delta", Delta: delta}
				}
				events <- &StreamEvent{Type: "message_stop"}
			} else {
				errc <- err
			}
			return
		}

		if raw.Data == "[DONE]" {
			// Finalize active tool calls.
			for idx, tc := range activeTools {
				events <- &StreamEvent{
					Type:  "content_block_stop",
					Index: idx,
				}
				_ = tc
			}
			if len(activeTools) > 0 {
				delta, _ := json.Marshal(map[string]string{"stop_reason": "tool_use"})
				events <- &StreamEvent{Type: "message_delta", Delta: delta}
			}
			events <- &StreamEvent{Type: "message_stop"}
			return
		}

		var chunk struct {
			Choices []struct {
				Delta        openaiStreamDelta `json:"delta"`
				FinishReason *string           `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw.Data), &chunk); err != nil {
			continue
		}

		// Handle usage data from final chunk (when choices is empty but usage is present).
		if chunk.Usage != nil && len(chunk.Choices) == 0 {
			// Emit message_start with usage for input tokens.
			inputTokens := chunk.Usage.InputTokens + chunk.Usage.PromptTokens
			if inputTokens > 0 {
				events <- &StreamEvent{
					Type: "message_start",
					Message: &Response{
						Usage: Usage{
							InputTokens: inputTokens,
						},
					},
				}
			}
			// Emit message_delta with usage for output tokens.
			outputTokens := chunk.Usage.OutputTokens + chunk.Usage.CompletionTokens
			if outputTokens > 0 {
				events <- &StreamEvent{
					Type: "message_delta",
					Usage: &Usage{
						OutputTokens: outputTokens,
					},
				}
			}
			continue
		}

		for _, choice := range chunk.Choices {
			// Handle reasoning/thinking content.
			if choice.Delta.ReasoningContent != "" {
				delta, _ := json.Marshal(map[string]string{
					"type":     "thinking_delta",
					"thinking": choice.Delta.ReasoningContent,
				})
				events <- &StreamEvent{
					Type:  "content_block_delta",
					Delta: delta,
				}
			}

			// Handle text content.
			if choice.Delta.Content != "" {
				delta, _ := json.Marshal(map[string]string{
					"type": "text_delta",
					"text": choice.Delta.Content,
				})
				events <- &StreamEvent{
					Type:  "content_block_delta",
					Delta: delta,
				}
			}

			// Handle tool calls.
			for _, tc := range choice.Delta.ToolCalls {
				state, exists := activeTools[tc.Index]
				if !exists {
					// New tool call — emit content_block_start.
					state = &toolCallState{
						id:   tc.ID,
						name: tc.Function.Name,
					}
					activeTools[tc.Index] = state

					block := &ContentBlock{
						Type: ContentTypeToolUse,
						ID:   tc.ID,
						Name: tc.Function.Name,
					}
					events <- &StreamEvent{
						Type:         "content_block_start",
						Index:        tc.Index,
						ContentBlock: block,
					}
				}

				// Accumulate arguments and emit partial_json delta.
				if tc.Function.Arguments != "" {
					state.args.WriteString(tc.Function.Arguments)
					delta, _ := json.Marshal(map[string]string{
						"type":         "input_json_delta",
						"partial_json": tc.Function.Arguments,
					})
					events <- &StreamEvent{
						Type:  "content_block_delta",
						Index: tc.Index,
						Delta: delta,
					}
				}
			}

			// Handle finish_reason.
			if choice.FinishReason != nil {
				reason := *choice.FinishReason
				// Finalize tool calls.
				for idx, tc := range activeTools {
					events <- &StreamEvent{
						Type:  "content_block_stop",
						Index: idx,
					}
					_ = tc
				}

				// Map OpenAI finish reasons to Anthropic stop reasons.
				stopReason := "end_turn"
				if reason == "tool_calls" || reason == "function_call" {
					stopReason = "tool_use"
				} else if reason == "length" {
					stopReason = "max_tokens"
				}
				delta, _ := json.Marshal(map[string]string{"stop_reason": stopReason})
				events <- &StreamEvent{Type: "message_delta", Delta: delta}

				// Clear active tools so EOF doesn't double-emit.
				activeTools = make(map[int]*toolCallState)
			}
		}
	}
}

func (c *OpenAICompatClient) readNonStream(body io.Reader, events chan<- *StreamEvent, errc chan<- error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content          string           `json:"content"`
				ReasoningContent string           `json:"reasoning_content"`
				ToolCalls        []openaiToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		errc <- fmt.Errorf("decode response: %w", err)
		return
	}

	for _, choice := range resp.Choices {
		// Emit reasoning/thinking content.
		if choice.Message.ReasoningContent != "" {
			delta, _ := json.Marshal(map[string]string{
				"type":     "thinking_delta",
				"thinking": choice.Message.ReasoningContent,
			})
			events <- &StreamEvent{
				Type:  "content_block_delta",
				Delta: delta,
			}
		}

		// Emit text content.
		if choice.Message.Content != "" {
			delta, _ := json.Marshal(map[string]string{
				"type": "text_delta",
				"text": choice.Message.Content,
			})
			events <- &StreamEvent{
				Type:  "content_block_delta",
				Delta: delta,
			}
		}

		// Emit tool calls.
		for i, tc := range choice.Message.ToolCalls {
			block := &ContentBlock{
				Type:  ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			}
			events <- &StreamEvent{
				Type:         "content_block_start",
				Index:        i,
				ContentBlock: block,
			}
			events <- &StreamEvent{
				Type:  "content_block_stop",
				Index: i,
			}
		}

		// Emit stop reason.
		stopReason := "end_turn"
		if choice.FinishReason == "tool_calls" || choice.FinishReason == "function_call" {
			stopReason = "tool_use"
		}
		delta, _ := json.Marshal(map[string]string{"stop_reason": stopReason})
		events <- &StreamEvent{Type: "message_delta", Delta: delta}
	}

	// Emit usage if provided.
	if resp.Usage != nil {
		// Emit message_start with input tokens (handle both Anthropic and OpenAI field names).
		inputTokens := resp.Usage.InputTokens + resp.Usage.PromptTokens
		if inputTokens > 0 {
			events <- &StreamEvent{
				Type: "message_start",
				Message: &Response{
					Usage: Usage{
						InputTokens: inputTokens,
					},
				},
			}
		}
		// Emit message_delta with output tokens (handle both Anthropic and OpenAI field names).
		outputTokens := resp.Usage.OutputTokens + resp.Usage.CompletionTokens
		if outputTokens > 0 {
			events <- &StreamEvent{
				Type: "message_delta",
				Usage: &Usage{
					OutputTokens: outputTokens,
				},
			}
		}
	}

	events <- &StreamEvent{Type: "message_stop"}
}
