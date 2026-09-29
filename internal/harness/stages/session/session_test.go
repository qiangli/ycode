package session

import (
	"context"
	"encoding/json"
	"fmt"
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

// charTokens counts about one token per four bytes of message content, so a
// large tool result costs what it would cost a model.
type charTokens struct{}

func (charTokens) CountMessages(messages []message.Message) (int, error) {
	total := 0
	for _, item := range messages {
		total += 4
		for _, block := range item.Content {
			total += (len(block.Text) + len(block.Content) + len(block.Input)) / 4
		}
	}
	return total, nil
}

func budgetEngine(t *testing.T, maxTokens int) (*Engine, *event.Store, *event.PayloadStore) {
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
	doc := &spec.Document{Spec: spec.Spec{Sessions: map[string]spec.Session{"durable": {History: spec.SessionHistory{MaxTokens: maxTokens, Unit: "turns", ClearToolResults: spec.SessionClearToolResults{OlderThanTurns: 4, Placeholder: "[omitted]"}, Repair: "tool-pairs"}}}}}
	engine, err := New(Config{Document: doc, Events: events, Payloads: payloads, Tokens: charTokens{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine, events, payloads
}

func toolPair(id, script, result string) []message.Message {
	return []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: id, Name: "bashy", Input: json.RawMessage(`{"script":"` + script + `"}`)}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: id, Content: result}}},
	}
}

func commitOne(t *testing.T, events *event.Store, payloads *event.PayloadStore, messages []message.Message) {
	t.Helper()
	ref := putMessages(t, payloads, messages)
	if _, err := events.Append(event.Draft{SessionID: "s", RunID: "r1", Type: "session.turn-committed", Data: map[string]any{"messages_ref": ref}}); err != nil {
		t.Fatal(err)
	}
}

// django__django-15280 (Sprint 322): a genie episode is ONE turn (one
// request, then every tool call of the agent loop). Its committed history
// (16 tool results, ~22k tokens) exceeded history maxTokens 20000, and
// session.load failed the resumed turn with "one turn exceeds history token
// budget". An oversized turn must be elided, never fatal: the request and the
// newest results stay, the oldest results give way first.
func TestLoadElidesOneOversizedTurnInsteadOfFailing(t *testing.T) {
	engine, events, payloads := budgetEngine(t, 20000)
	committed := []message.Message{textMsg(message.RoleUser, "Solve this SWE-bench issue")}
	for i := 0; i < 16; i++ {
		committed = append(committed, toolPair(fmt.Sprintf("call-%02d", i), "sed -n 1,220p file.py", fmt.Sprintf("result %02d\n", i)+strings.Repeat("x = 1\n", 1000))...)
	}
	committed = append(committed, textMsg(message.RoleAssistant, "I'll continue from the prior inspection."))
	commitOne(t, events, payloads, committed)
	if total, _ := (charTokens{}).CountMessages(committed); total <= 20000 {
		t.Fatalf("fixture must exceed the budget, got %d tokens", total)
	}
	result, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "s", Events: replay(t, eventsPath(t))})
	if err != nil {
		t.Fatalf("an oversized turn must not fail session.load: %v", err)
	}
	if result.Tokens > 20000 {
		t.Fatalf("projected history = %d tokens, want <= 20000", result.Tokens)
	}
	got := result.Messages
	if got[0].Content[0].Text != "Solve this SWE-bench issue" {
		t.Fatalf("the turn's request must stay first: %#v", got[0])
	}
	last := got[len(got)-1]
	if last.Role != message.RoleAssistant || !strings.Contains(messageText(last), "continue") {
		t.Fatalf("the newest message must stay: %#v", last)
	}
	newest := got[len(got)-2]
	if !strings.Contains(newest.Content[0].Content, "result 15") {
		t.Fatalf("the newest tool result must stay intact: %.80q", newest.Content[0].Content)
	}
	if first := got[2]; first.Content[0].Content != "[omitted]" {
		t.Fatalf("the oldest tool result must give way first: %.80q", first.Content[0].Content)
	}
	assertPairedToolResults(t, got)
	loaded := lastEvent(t, "session.history.loaded")
	if !strings.Contains(string(loaded.Data), `"elided"`) {
		t.Fatalf("history.loaded must record the elision: %s", loaded.Data)
	}
}

// One tool result alone bigger than the whole budget: its middle is elided
// with a note, the head and tail stay, and the load still succeeds.
func TestLoadElidesASingleResultLargerThanTheBudget(t *testing.T) {
	engine, events, payloads := budgetEngine(t, 2000)
	huge := "HEAD LINE\n" + strings.Repeat("log line of a full test run\n", 4000) + "TAIL LINE\n"
	committed := append([]message.Message{textMsg(message.RoleUser, "run the tests")}, toolPair("only", "./runtests.py", huge)...)
	committed = append(committed, textMsg(message.RoleAssistant, "done"))
	commitOne(t, events, payloads, committed)
	result, err := engine.Load(context.Background(), testMeta(), LoadRequest{SessionRef: "durable", SessionID: "s", Events: replay(t, eventsPath(t))})
	if err != nil {
		t.Fatalf("an oversized result must not fail session.load: %v", err)
	}
	if result.Tokens > 2000 {
		t.Fatalf("projected history = %d tokens, want <= 2000", result.Tokens)
	}
	var kept string
	for _, item := range result.Messages {
		for _, block := range item.Content {
			if block.Type == message.ContentTypeToolResult {
				kept = block.Content
			}
		}
	}
	if !strings.HasPrefix(kept, "HEAD LINE") || !strings.HasSuffix(kept, "TAIL LINE\n") || !strings.Contains(kept, "[output truncated:") {
		t.Fatalf("the result must keep head and tail around a truncation note: %.120q ... %.60q", kept, kept[max(0, len(kept)-60):])
	}
	assertPairedToolResults(t, result.Messages)
}

func assertPairedToolResults(t *testing.T, messages []message.Message) {
	t.Helper()
	open := map[string]bool{}
	for _, item := range messages {
		for _, block := range item.Content {
			switch block.Type {
			case message.ContentTypeToolUse:
				open[block.ID] = true
			case message.ContentTypeToolResult:
				if !open[block.ToolUseID] {
					t.Fatalf("tool result %q has no tool use", block.ToolUseID)
				}
				delete(open, block.ToolUseID)
			}
		}
	}
	if len(open) != 0 {
		t.Fatalf("tool uses without results: %v", open)
	}
}

func lastEvent(t *testing.T, kind string) event.Event {
	t.Helper()
	all := replay(t, eventsPath(t))
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Type == kind {
			return all[i]
		}
	}
	t.Fatalf("no %s event", kind)
	return event.Event{}
}
