// Package session projects durable session history from the canonical event log.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
)

const SchemaVersion = "ycode.session/v1"

type TokenCounter interface {
	CountMessages([]message.Message) (int, error)
}

type Config struct {
	Document *spec.Document
	Events   *event.Store
	Payloads *event.PayloadStore
	Tokens   TokenCounter
}

type Engine struct {
	sessions map[string]spec.Session
	events   *event.Store
	payloads *event.PayloadStore
	tokens   TokenCounter
}

type Meta struct {
	SessionID    string
	RunID        string
	StageID      string
	ConfigDigest string
}

func (m Meta) validate() error {
	if m.SessionID == "" || m.RunID == "" || m.StageID == "" || m.ConfigDigest == "" {
		return errors.New("session stage requires session, run, stage and config digest")
	}
	return nil
}

type LoadRequest struct {
	SessionRef string
	SessionID  string
	Events     []event.Event
}

type LoadResult struct {
	SchemaVersion string            `json:"schema_version"`
	MessagesRef   string            `json:"messages_ref"`
	MessageCount  int               `json:"message_count"`
	Tokens        int               `json:"tokens"`
	Source        string            `json:"source"`
	Messages      []message.Message `json:"-"`
}

type CommitRequest struct {
	SessionRef  string
	Messages    []message.Message
	Compactions int
}

type CommitResult struct {
	SchemaVersion string `json:"schema_version"`
	MessagesRef   string `json:"messages_ref"`
	MessageCount  int    `json:"message_count"`
	Tokens        int    `json:"tokens"`
	Compactions   int    `json:"compactions"`
}

func New(config Config) (*Engine, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.Tokens == nil {
		return nil, errors.New("session engine requires compiled document, events, payloads and token counter")
	}
	return &Engine{sessions: cloneSessions(config.Document.Spec.Sessions), events: config.Events, payloads: config.Payloads, tokens: config.Tokens}, nil
}

func (e *Engine) Load(_ context.Context, meta Meta, request LoadRequest) (LoadResult, error) {
	if err := meta.validate(); err != nil {
		return LoadResult{}, err
	}
	policy, err := e.historyPolicy(request.SessionRef)
	if err != nil {
		return LoadResult{}, err
	}
	ref, source, err := e.historyRef(request.SessionID, request.Events)
	if err != nil {
		return LoadResult{}, err
	}
	messages, err := e.readMessages(ref)
	if err != nil {
		return LoadResult{}, err
	}
	projected, err := e.project(messages, policy)
	if err != nil {
		return LoadResult{}, err
	}
	tokens, err := e.tokens.CountMessages(projected)
	if err != nil || tokens < 0 {
		return LoadResult{}, errors.New("session.load: token measurement failed")
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return LoadResult{}, err
	}
	projectedRef, err := e.payloads.Put(encoded)
	if err != nil {
		return LoadResult{}, err
	}
	result := LoadResult{SchemaVersion: SchemaVersion, MessagesRef: projectedRef, MessageCount: len(projected), Tokens: tokens, Source: source, Messages: projected}
	_, err = e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: "session.history.loaded", ConfigDigest: meta.ConfigDigest, Data: map[string]any{"schema_version": result.SchemaVersion, "messages_ref": result.MessagesRef, "message_count": result.MessageCount, "tokens": result.Tokens, "source": result.Source}})
	return result, err
}

func (e *Engine) Commit(_ context.Context, meta Meta, request CommitRequest) (CommitResult, error) {
	if err := meta.validate(); err != nil {
		return CommitResult{}, err
	}
	if _, err := e.historyPolicy(request.SessionRef); err != nil {
		return CommitResult{}, err
	}
	messages := cloneMessages(request.Messages)
	tokens, err := e.tokens.CountMessages(messages)
	if err != nil || tokens < 0 {
		return CommitResult{}, errors.New("session.commit: token measurement failed")
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return CommitResult{}, err
	}
	ref, err := e.payloads.Put(raw)
	if err != nil {
		return CommitResult{}, err
	}
	result := CommitResult{SchemaVersion: SchemaVersion, MessagesRef: ref, MessageCount: len(messages), Tokens: tokens, Compactions: request.Compactions}
	_, err = e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: "session.turn-committed", ConfigDigest: meta.ConfigDigest, Data: result})
	return result, err
}

func (e *Engine) historyPolicy(ref string) (spec.SessionHistory, error) {
	configured, ok := e.sessions[ref]
	if !ok {
		return spec.SessionHistory{}, fmt.Errorf("session history: undeclared session %q", ref)
	}
	policy := configured.History
	if policy.MaxTokens <= 0 || policy.Unit != "turns" || policy.Repair != "tool-pairs" {
		return spec.SessionHistory{}, fmt.Errorf("session history: session %q is absent or unbounded", ref)
	}
	if policy.ClearToolResults.OlderThanTurns < 0 || policy.ClearToolResults.Placeholder == "" {
		return spec.SessionHistory{}, fmt.Errorf("session history: session %q clearToolResults policy is incomplete", ref)
	}
	return policy, nil
}

func (e *Engine) historyRef(sessionID string, events []event.Event) (string, string, error) {
	if sessionID == "" {
		return "", "", errors.New("session.load: session id is required")
	}
	var forkRef string
	for i := len(events) - 1; i >= 0; i-- {
		item := events[i]
		if item.SessionID != sessionID {
			continue
		}
		switch item.Type {
		case "session.turn-committed":
			var data struct {
				MessagesRef string `json:"messages_ref"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				return "", "", err
			}
			if data.MessagesRef == "" {
				return "", "", errors.New("session.load: committed turn has no messages_ref")
			}
			return data.MessagesRef, "turn-committed", nil
		case "session.forked":
			var data struct {
				ParentMessagesRef string `json:"parent_messages_ref"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				return "", "", err
			}
			forkRef = data.ParentMessagesRef
		}
	}
	if forkRef != "" {
		return forkRef, "fork-seed", nil
	}
	return "", "empty", nil
}

func (e *Engine) readMessages(ref string) ([]message.Message, error) {
	if ref == "" {
		return nil, nil
	}
	raw, err := e.payloads.Get(ref)
	if err != nil {
		return nil, err
	}
	var messages []message.Message
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (e *Engine) project(messages []message.Message, policy spec.SessionHistory) ([]message.Message, error) {
	filtered := make([]message.Message, 0, len(messages))
	for _, item := range messages {
		if item.Role == message.RoleSystem {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = clearToolResults(filtered, policy.ClearToolResults.OlderThanTurns, policy.ClearToolResults.Placeholder)
	filtered, err := e.trimWholeTurns(filtered, policy.MaxTokens)
	if err != nil {
		return nil, err
	}
	return repairToolPairs(filtered, policy.ClearToolResults.Placeholder), nil
}

func (e *Engine) trimWholeTurns(messages []message.Message, maxTokens int) ([]message.Message, error) {
	current := cloneMessages(messages)
	for {
		tokens, err := e.tokens.CountMessages(current)
		if err != nil || tokens < 0 {
			return nil, errors.New("session.load: token measurement failed")
		}
		if tokens <= maxTokens || len(current) == 0 {
			return current, nil
		}
		next := nextTurnStart(current, 1)
		if next <= 0 || next >= len(current) {
			return nil, errors.New("session.load: one turn exceeds history token budget")
		}
		current = current[next:]
	}
}

func clearToolResults(messages []message.Message, olderThanTurns int, placeholder string) []message.Message {
	out := cloneMessages(messages)
	turnIndex := make([]int, len(out))
	turns := 0
	for i := range out {
		if isTurnStart(out[i]) {
			turns++
		}
		turnIndex[i] = turns
	}
	clearThrough := turns - olderThanTurns
	for i := range out {
		if turnIndex[i] == 0 || turnIndex[i] > clearThrough {
			continue
		}
		for j := range out[i].Content {
			if out[i].Content[j].Type == message.ContentTypeToolResult {
				out[i].Content[j].Content = placeholder
				out[i].Content[j].IsError = false
			}
		}
	}
	return out
}

func repairToolPairs(messages []message.Message, placeholder string) []message.Message {
	out := make([]message.Message, 0, len(messages))
	open := map[string]struct{}{}
	for _, item := range cloneMessages(messages) {
		kept := item.Content[:0]
		for _, block := range item.Content {
			switch block.Type {
			case message.ContentTypeToolUse:
				if block.ID != "" {
					open[block.ID] = struct{}{}
				}
				kept = append(kept, block)
			case message.ContentTypeToolResult:
				if _, ok := open[block.ToolUseID]; ok {
					delete(open, block.ToolUseID)
					kept = append(kept, block)
				}
			default:
				kept = append(kept, block)
			}
		}
		if len(kept) == 0 {
			continue
		}
		item.Content = kept
		out = append(out, item)
	}
	if len(open) == 0 {
		return out
	}
	repaired := make([]message.Message, 0, len(out)+len(open))
	for _, item := range out {
		repaired = append(repaired, item)
		for _, block := range item.Content {
			if block.Type != message.ContentTypeToolUse || block.ID == "" {
				continue
			}
			if _, ok := open[block.ID]; !ok {
				continue
			}
			delete(open, block.ID)
			repaired = append(repaired, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: block.ID, Content: placeholder, IsError: false}}})
		}
	}
	return repaired
}

func nextTurnStart(messages []message.Message, start int) int {
	for i := start; i < len(messages); i++ {
		if isTurnStart(messages[i]) {
			return i
		}
	}
	return len(messages)
}

func isTurnStart(item message.Message) bool {
	if item.Role != message.RoleUser {
		return false
	}
	for _, block := range item.Content {
		if block.Type == message.ContentTypeToolResult {
			return false
		}
	}
	return true
}

func cloneSessions(in map[string]spec.Session) map[string]spec.Session {
	out := make(map[string]spec.Session, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneMessages(messages []message.Message) []message.Message {
	raw, _ := json.Marshal(messages)
	var result []message.Message
	_ = json.Unmarshal(raw, &result)
	return result
}
