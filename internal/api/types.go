package api

import "encoding/json"

// MessageRole represents the role of a message participant.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ContentType identifies the kind of content block.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeThinking   ContentType = "thinking"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
	ContentTypeImage      ContentType = "image"
)

// CacheControl marks a content block for provider-level prompt caching.
// When set to {"type":"ephemeral"}, Anthropic caches the prefix up to and
// including this block, yielding ~90% cost reduction on cache-hit tokens.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// ContentBlock represents a single block of content within a message.
type ContentBlock struct {
	Type ContentType `json:"type"`

	// Text content (type == "text")
	Text string `json:"text,omitempty"`

	// Tool use (type == "tool_use")
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// Tool result (type == "tool_result")
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	// Image (type == "image")
	Source *ImageSource `json:"source,omitempty"`

	// CacheControl marks this block as a cache breakpoint for providers
	// that support prompt caching (Anthropic).
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource for image content blocks.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", etc.
	Data      string `json:"data"`
}

// Message is a single message in the conversation.
type Message struct {
	Role    MessageRole    `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// SystemBlock is a content block used in the array form of the "system" field.
// Anthropic accepts system as either a plain string or an array of these blocks,
// which allows attaching cache_control markers.
type SystemBlock struct {
	Type         string        `json:"type"`                    // "text"
	Text         string        `json:"text"`                    //
	CacheControl *CacheControl `json:"cache_control,omitempty"` //
}

// Request is the API request to send to a provider.
type Request struct {
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens"`
	System      string           `json:"system,omitempty"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Stream      bool             `json:"stream"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	StopReason  string           `json:"-"`

	// ReasoningEffort controls how much "thinking" the model does.
	// Values: "low", "medium", "high", or "" (provider default).
	// Lower effort = fewer thinking tokens = faster + cheaper.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// SystemBlocks is the array form of the system prompt, used when cache
	// control markers are needed (Anthropic). When non-nil, it takes
	// precedence over the plain System string during JSON marshaling.
	SystemBlocks []SystemBlock `json:"-"`
}

// MarshalJSON implements custom marshaling for Request. When SystemBlocks is
// populated, the "system" field is serialized as an array of content blocks
// (required for Anthropic prompt caching). Otherwise falls back to the plain
// string form.
func (r Request) MarshalJSON() ([]byte, error) {
	// Use an alias to avoid infinite recursion.
	type requestAlias Request
	if len(r.SystemBlocks) == 0 {
		return json.Marshal(requestAlias(r))
	}

	// Build a map from the alias, then replace "system" with the blocks array.
	data, err := json.Marshal(requestAlias(r))
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	blocks, err := json.Marshal(r.SystemBlocks)
	if err != nil {
		return nil, err
	}
	m["system"] = blocks
	return json.Marshal(m)
}

// Response is the full API response.
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         MessageRole    `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens        int `json:"input_tokens,omitempty"`
	OutputTokens       int `json:"output_tokens,omitempty"`
	PromptTokens       int `json:"prompt_tokens,omitempty"`
	CompletionTokens   int `json:"completion_tokens,omitempty"`
	CacheCreationInput int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInput     int `json:"cache_read_input_tokens,omitempty"`
}

// StreamEvent represents a single SSE event from the streaming API.
type StreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`
	Delta json.RawMessage `json:"delta,omitempty"`

	// Parsed from SSE data fields.
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Message      *Response     `json:"message,omitempty"`
	Usage        *Usage        `json:"usage,omitempty"`
}

// StopReason constants.
const (
	StopReasonEndTurn   = "end_turn"
	StopReasonToolUse   = "tool_use"
	StopReasonMaxTokens = "max_tokens"
	StopReasonStop      = "stop_sequence"
)
