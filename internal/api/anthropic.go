package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultAnthropicURL     = "https://api.anthropic.com/v1/messages"
	defaultAnthropicVersion = "2023-06-01"
)

// AnthropicClient implements Provider for the Anthropic Messages API.
type AnthropicClient struct {
	apiKey      string
	bearerToken string
	baseURL     string
	apiVersion  string
	httpClient  *http.Client
}

// AnthropicOption configures the Anthropic client.
type AnthropicOption func(*AnthropicClient)

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) AnthropicOption {
	return func(c *AnthropicClient) { c.baseURL = url }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) AnthropicOption {
	return func(c *AnthropicClient) { c.httpClient = hc }
}

// WithBearerToken sets a bearer token for OAuth authentication.
func WithBearerToken(token string) AnthropicOption {
	return func(c *AnthropicClient) { c.bearerToken = token }
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(apiKey string, opts ...AnthropicOption) *AnthropicClient {
	c := &AnthropicClient{
		apiKey:     apiKey,
		baseURL:    defaultAnthropicURL,
		apiVersion: defaultAnthropicVersion,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *AnthropicClient) Kind() ProviderKind {
	return ProviderAnthropic
}

// applyCacheMarks adds Anthropic prompt caching annotations to the request.
// It marks:
//   - The system prompt with cache_control on its last block
//   - The last 2 non-system messages' final content blocks
//
// This follows the same strategy as opencode's applyCaching in transform.ts.
func applyCacheMarks(req *Request) {
	ephemeral := &CacheControl{Type: "ephemeral"}

	// Convert plain system string to SystemBlocks with cache mark.
	if req.System != "" && len(req.SystemBlocks) == 0 {
		req.SystemBlocks = []SystemBlock{
			{Type: "text", Text: req.System, CacheControl: ephemeral},
		}
	} else if len(req.SystemBlocks) > 0 {
		// Mark the last system block.
		req.SystemBlocks[len(req.SystemBlocks)-1].CacheControl = ephemeral
	}

	// Mark the last content block of the last 2 messages.
	marked := 0
	for i := len(req.Messages) - 1; i >= 0 && marked < 2; i-- {
		blocks := req.Messages[i].Content
		if len(blocks) == 0 {
			continue
		}
		blocks[len(blocks)-1].CacheControl = ephemeral
		marked++
	}
}

// Send sends a streaming request to the Anthropic API.
func (c *AnthropicClient) Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
	events := make(chan *StreamEvent, 16)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		req.Stream = true
		applyCacheMarks(req)
		body, err := json.Marshal(req)
		if err != nil {
			errc <- fmt.Errorf("marshal request: %w", err)
			return
		}

		// Compress request body if beneficial (typically 60-80% reduction).
		compressedBody, contentEncoding := CompressGzip(body)

		makeReq := func() (*http.Request, error) {
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(compressedBody))
			if err != nil {
				return nil, fmt.Errorf("create request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")
			if contentEncoding != "" {
				httpReq.Header.Set("Content-Encoding", contentEncoding)
			}
			if c.apiKey != "" {
				httpReq.Header.Set("X-API-Key", c.apiKey)
			}
			if c.bearerToken != "" {
				httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
			}
			httpReq.Header.Set("anthropic-version", c.apiVersion)
			return httpReq, nil
		}

		resp, err := doWithRetry(ctx, c.httpClient, makeReq)
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()

		parser := NewSSEParser(resp.Body)
		for {
			raw, err := parser.Next()
			if err != nil {
				if err != io.EOF {
					errc <- fmt.Errorf("read SSE: %w", err)
				}
				return
			}

			se, err := ParseStreamEvent(raw)
			if err != nil {
				errc <- err
				return
			}

			select {
			case events <- se:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}

			// message_stop signals the end of the stream.
			if se.Type == "message_stop" {
				return
			}
		}
	}()

	return events, errc
}
