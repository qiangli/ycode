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
	projected, elided, err := e.project(messages, policy)
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
	data := map[string]any{"schema_version": result.SchemaVersion, "messages_ref": result.MessagesRef, "message_count": result.MessageCount, "tokens": result.Tokens, "source": result.Source}
	if elided.any() {
		data["elided"] = elided
	}
	_, err = e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: "session.history.loaded", ConfigDigest: meta.ConfigDigest, Data: data})
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

func (e *Engine) project(messages []message.Message, policy spec.SessionHistory) ([]message.Message, Elision, error) {
	filtered := make([]message.Message, 0, len(messages))
	for _, item := range messages {
		if item.Role == message.RoleSystem {
			continue
		}
		filtered = append(filtered, item)
	}
	placeholder := policy.ClearToolResults.Placeholder
	filtered = clearToolResults(filtered, policy.ClearToolResults.OlderThanTurns, placeholder)
	filtered, err := e.trimWholeTurns(filtered, policy.MaxTokens)
	if err != nil {
		return nil, Elision{}, err
	}
	// Whole turns are the unit of history, but one turn can outgrow the
	// budget by itself (an agent loop is one turn: one request, then every
	// tool call). Elide inside it rather than fail the next turn.
	filtered, elided, err := e.elideWithinTurn(filtered, policy.MaxTokens, placeholder)
	if err != nil {
		return nil, Elision{}, err
	}
	return repairToolPairs(filtered, placeholder), elided, nil
}

// trimWholeTurns drops the oldest whole turns until the history fits. It
// always keeps the newest turn, which elideWithinTurn bounds if needed.
func (e *Engine) trimWholeTurns(messages []message.Message, maxTokens int) ([]message.Message, error) {
	current := cloneMessages(messages)
	for {
		tokens, err := e.count(current)
		if err != nil {
			return nil, err
		}
		if tokens <= maxTokens || len(current) == 0 {
			return current, nil
		}
		next := nextTurnStart(current, 1)
		if next <= 0 || next >= len(current) {
			return current, nil
		}
		current = current[next:]
	}
}

// Elision records what session.load left out of a turn larger than the
// history budget.
type Elision struct {
	ToolResults     int `json:"tool_results,omitempty"`
	Messages        int `json:"messages,omitempty"`
	TruncatedBlocks int `json:"truncated_blocks,omitempty"`
}

func (e Elision) any() bool { return e.ToolResults+e.Messages+e.TruncatedBlocks > 0 }

// minElidedBlockBytes is the floor below which a block is not truncated
// further: a head and tail this small still say what the block was.
const minElidedBlockBytes = 512

// elideWithinTurn bounds a single turn that exceeds maxTokens, in order of
// least loss: (1) replace tool results with the placeholder, oldest first,
// (2) drop the oldest messages after the turn's opening request, (3) cut the
// middle of the largest remaining blocks. The newest message and the request
// are kept. It never fails for size: a history that cannot shrink further is
// returned as small as it got.
func (e *Engine) elideWithinTurn(messages []message.Message, maxTokens int, placeholder string) ([]message.Message, Elision, error) {
	var elided Elision
	tokens, err := e.count(messages)
	if err != nil || tokens <= maxTokens || len(messages) == 0 {
		return messages, elided, err
	}
	current := cloneMessages(messages)
	// (1) oldest tool results first. The newest result is spared: step (3)
	// keeps its head and tail instead of the placeholder.
	newest := len(current) - 1
	for newest > 0 && !hasToolResult(current[newest]) {
		newest--
	}
	for i := 1; i < newest && tokens > maxTokens; i++ {
		changed := false
		for j := range current[i].Content {
			block := &current[i].Content[j]
			if block.Type == message.ContentTypeToolResult && block.Content != placeholder {
				block.Content, block.IsError = placeholder, false
				elided.ToolResults++
				changed = true
			}
		}
		if changed {
			if tokens, err = e.count(current); err != nil {
				return nil, elided, err
			}
		}
	}
	// (2) oldest messages after the request, a tool call with its results,
	// up to the newest result's call (or the newest message).
	keepFrom := len(current) - 1
	if newest > 0 {
		keepFrom = newest
		if current[newest-1].Role == message.RoleAssistant {
			keepFrom = newest - 1
		}
	}
	for keepFrom > 1 && tokens > maxTokens {
		drop := 1
		for 1+drop < keepFrom && isToolResultMessage(current[1+drop]) {
			drop++
		}
		current = append(current[:1], current[1+drop:]...)
		keepFrom -= drop
		elided.Messages += drop
		if tokens, err = e.count(current); err != nil {
			return nil, elided, err
		}
	}
	// (3) the largest block's middle, until it fits or nothing is left to cut.
	for tokens > maxTokens {
		i, j, size := largestBlock(current)
		if size <= minElidedBlockBytes {
			break
		}
		block := &current[i].Content[j]
		if block.Type == message.ContentTypeToolResult {
			block.Content, _ = message.ElideMiddle(block.Content, size/2)
		} else {
			block.Text, _ = message.ElideMiddle(block.Text, size/2)
		}
		elided.TruncatedBlocks++
		if tokens, err = e.count(current); err != nil {
			return nil, elided, err
		}
	}
	return current, elided, nil
}

func (e *Engine) count(messages []message.Message) (int, error) {
	tokens, err := e.tokens.CountMessages(messages)
	if err != nil || tokens < 0 {
		return 0, errors.New("session.load: token measurement failed")
	}
	return tokens, nil
}

func hasToolResult(item message.Message) bool {
	for _, block := range item.Content {
		if block.Type == message.ContentTypeToolResult {
			return true
		}
	}
	return false
}

func isToolResultMessage(item message.Message) bool {
	if item.Role != message.RoleUser || len(item.Content) == 0 {
		return false
	}
	for _, block := range item.Content {
		if block.Type != message.ContentTypeToolResult {
			return false
		}
	}
	return true
}

// largestBlock finds the biggest text or tool-result block (by bytes).
func largestBlock(messages []message.Message) (int, int, int) {
	bi, bj, best := 0, 0, 0
	for i := range messages {
		for j, block := range messages[i].Content {
			size := len(block.Text)
			if block.Type == message.ContentTypeToolResult {
				size = len(block.Content)
			}
			if size > best {
				bi, bj, best = i, j, size
			}
		}
	}
	return bi, bj, best
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
