package session

import (
	"encoding/json"
	"time"
)

// MessageRole represents the role of a message participant.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// ContentType identifies the kind of content block.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

// ContentBlock represents a single block of content within a message.
type ContentBlock struct {
	Type ContentType `json:"type"`

	// Text content
	Text string `json:"text,omitempty"`

	// Tool use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// Tool result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ConversationMessage is a message in a conversation with metadata.
type ConversationMessage struct {
	UUID      string         `json:"uuid"`
	Role      MessageRole    `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp time.Time      `json:"timestamp"`
	Model     string         `json:"model,omitempty"`
	Usage     *TokenUsage    `json:"usage,omitempty"`
}

// TokenUsage tracks token counts for a message.
type TokenUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CacheCreationInput int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInput     int `json:"cache_read_input_tokens,omitempty"`
}
