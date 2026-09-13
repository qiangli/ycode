package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestLoadProjectionOrderSystemExclusionToolRepairAndClearing(t *testing.T) {
	engine, events, payloads := testEngine(t, 200)
	committed := []message.Message{
		textMsg(message.RoleSystem, "fresh context"),
		textMsg(message.RoleUser, "first"),
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: "old-call", Name: "bashy"}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: "old-call", Content: "old secret"}}},
		textMsg(message.RoleUser, "second"),
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: "missing-result", Name: "bashy"}}},
		textMsg(message.RoleAssistant, "done"),
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: "orphan", Content: "drop me"}}},
	}
	ref := putMessages(t, payloads, committed)
	if _, err := events.Append(event.Draft{SessionID: "s", RunID: "r1", Type: "session.turn-committed", Data: map[string]any{"messages_ref": ref}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "s", Events: replay(t, eventsPath(t))})
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages
	if len(got) != 7 {
		t.Fatalf("messages = %#v", got)
	}
	if got[0].Role != message.RoleUser || got[0].Content[0].Text != "first" {
		t.Fatalf("order/system filter = %#v", got)
	}
	if got[2].Content[0].Type != message.ContentTypeToolResult || got[2].Content[0].Content != "[omitted]" {
		t.Fatalf("old tool result was not cleared: %#v", got[2])
	}
	if got[5].Content[0].Type != message.ContentTypeToolResult || got[5].Content[0].ToolUseID != "missing-result" {
		t.Fatalf("missing tool result was not synthesized: %#v", got)
	}
	for _, item := range got {
		if item.Role == message.RoleSystem || strings.Contains(messageText(item), "orphan") {
			t.Fatalf("projection kept excluded content: %#v", got)
		}
	}
}

func TestLoadCutsOnlyWholeTurns(t *testing.T) {
	engine, events, payloads := testEngine(t, 4)
	ref := putMessages(t, payloads, []message.Message{
		textMsg(message.RoleUser, "first"),
		textMsg(message.RoleAssistant, "first answer"),
		textMsg(message.RoleUser, "second"),
		textMsg(message.RoleAssistant, "second answer"),
		textMsg(message.RoleUser, "third"),
		textMsg(message.RoleAssistant, "third answer"),
	})
	if _, err := events.Append(event.Draft{SessionID: "s", RunID: "r1", Type: "session.turn-committed", Data: map[string]any{"messages_ref": ref}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "s", Events: replay(t, eventsPath(t))})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 4 || result.Messages[0].Content[0].Text != "second" {
		t.Fatalf("whole-turn trim = %#v", result.Messages)
	}
}

func TestLoadUsesForkSeedWhenChildHasNoTurns(t *testing.T) {
	engine, events, payloads := testEngine(t, 200)
	ref := putMessages(t, payloads, []message.Message{textMsg(message.RoleUser, "parent"), textMsg(message.RoleAssistant, "answer")})
	if _, err := events.Append(event.Draft{SessionID: "child", RunID: "fork", Type: "session.forked", Data: map[string]any{"parent_messages_ref": ref}}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "child", Events: replay(t, eventsPath(t))})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "fork-seed" || len(result.Messages) != 2 || result.Messages[0].Content[0].Text != "parent" {
		t.Fatalf("fork seed = %#v", result)
	}
}

func TestLoadFailsClosedWithoutBoundedHistoryPolicy(t *testing.T) {
	isolateStores(t)
	dir := t.TempDir()
	events, err := event.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Document: &spec.Document{Spec: spec.Spec{Sessions: map[string]spec.Session{"durable": {}}}}, Events: events, Payloads: payloads, Tokens: fixedTokens{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "s"}); err == nil || !strings.Contains(err.Error(), "absent or unbounded") {
		t.Fatalf("err = %v", err)
	}
}

func testEngine(t *testing.T, maxTokens int) (*Engine, *event.Store, *event.PayloadStore) {
	t.Helper()
	isolateStores(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	t.Setenv("SESSION_TEST_EVENTS", path)
	events, err := event.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	doc := &spec.Document{Spec: spec.Spec{Sessions: map[string]spec.Session{"durable": {History: spec.SessionHistory{MaxTokens: maxTokens, Unit: "turns", ClearToolResults: spec.SessionClearToolResults{OlderThanTurns: 1, Placeholder: "[omitted]"}, Repair: "tool-pairs"}}}}}
	engine, err := New(Config{Document: doc, Events: events, Payloads: payloads, Tokens: fixedTokens{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine, events, payloads
}

func isolateStores(t *testing.T) {
	t.Helper()
	for _, name := range []string{"BASHY_KB_DIR", "BASHY_HOME", "BASHY_SKILLS_DIR", "YCODE_DATA_DIR"} {
		t.Setenv(name, filepath.Join(t.TempDir(), name))
	}
}

func eventsPath(t *testing.T) string {
	t.Helper()
	return os.Getenv("SESSION_TEST_EVENTS")
}

func replay(t *testing.T, path string) []event.Event {
	t.Helper()
	events, err := event.Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func putMessages(t *testing.T, payloads *event.PayloadStore, messages []message.Message) string {
	t.Helper()
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := payloads.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func testMeta() Meta {
	return Meta{SessionID: "s", RunID: "r2", StageID: "session", ConfigDigest: "sha256:c"}
}

func textMsg(role message.Role, text string) message.Message {
	return message.Message{Role: role, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: text}}}
}

type fixedTokens struct{}

func (fixedTokens) CountMessages(messages []message.Message) (int, error) { return len(messages), nil }

func messageText(item message.Message) string {
	var out []string
	for _, block := range item.Content {
		out = append(out, block.Text, block.Content)
	}
	return strings.Join(out, " ")
}
