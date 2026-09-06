// Package message defines the neutral message value exchanged by YAML harness
// stages. It deliberately contains no session persistence or conversation-loop
// policy.
package message

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

type ContentBlock struct {
	Type      ContentType     `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type Message struct {
	UUID      string         `json:"uuid"`
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp time.Time      `json:"timestamp"`
	Model     string         `json:"model,omitempty"`
	Usage     *TokenUsage    `json:"usage,omitempty"`
}

type TokenUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CacheCreationInput int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInput     int `json:"cache_read_input_tokens,omitempty"`
}
