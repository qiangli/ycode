package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
)

// Sprint 412 Story #896: a declared model effort is useless unless it reaches
// the provider request, and the same is true of a declared fragment cache
// break. This runs one compiled turn against the canonical fixture and reads
// the request the provider actually received.
func TestTurnCarriesDeclaredEffortAndCacheBreaksToTheProvider(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".effort-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "      id: gpt-5.6", "      id: gpt-5.6\n      effort: xhigh", 1)
	if edited == string(raw) {
		t.Fatal("fixture no longer declares the model id this test edits")
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), []byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	backend := &provider.MockBackend{Events: completedWireEvents("done")}
	adapter, err := provider.NewMock(backend)
	if err != nil {
		t.Fatal(err)
	}
	events, err := event.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: &recordingDelivery{}, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": adapter}, Queue: emptyQueue{}, EventPath: filepath.Join(root, "events.jsonl"), Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	inputBytes := []byte(`{"request":"carry the declared policy"}`)
	inputRef, err := payloads.Put(inputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(context.Background(), Request{SessionID: "session", RunID: "run-1", OriginFrontend: "embed", Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "test", IdempotencyKey: "run-1", Data: inputBytes, PayloadRef: inputRef}}); err != nil {
		t.Fatal(err)
	}

	sent := backend.LastRequest()
	if sent == nil {
		t.Fatal("provider received no request")
	}
	if sent.ReasoningEffort != "xhigh" {
		t.Fatalf("ReasoningEffort = %q, want xhigh", sent.ReasoningEffort)
	}
	// examples/agent.yaml declares its identity fragment with
	// cache.breakAfter: true, so the system prompt must arrive segmented at
	// that break instead of flattened into one opaque string. (The project
	// fragment is optional and resolves empty in a scratch workspace; an
	// empty segment is never a block, since Anthropic rejects one.)
	if len(sent.SystemBlocks) == 0 || !sent.SystemBlocks[0].CacheBreak {
		t.Fatalf("SystemBlocks = %#v, want the declared cache break preserved", sent.SystemBlocks)
	}
	for i, block := range sent.SystemBlocks {
		if block.Type != "text" || block.Text == "" {
			t.Fatalf("SystemBlocks[%d] = %#v", i, block)
		}
	}
	texts := make([]string, 0, len(sent.SystemBlocks))
	for _, block := range sent.SystemBlocks {
		texts = append(texts, block.Text)
	}
	if joined := strings.Join(texts, "\n\n"); joined != sent.System {
		t.Fatalf("segmented system prompt changed the text:\n%q\n%q", joined, sent.System)
	}
}

// providerMessages segments the system prompt at declared cache breaks while
// keeping the flattened text identical, and leaves conversation messages
// untouched.
func TestProviderMessagesSegmentsSystemAtCacheBreaks(t *testing.T) {
	messages := []message.Message{
		{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "identity", CacheBreak: true}}},
		{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "project"}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "hi"}}},
	}
	conversation, blocks := providerMessages(messages)
	if len(conversation) != 1 || conversation[0].Role != api.RoleUser {
		t.Fatalf("conversation = %#v", conversation)
	}
	want := []api.SystemBlock{
		{Type: "text", Text: "identity", CacheBreak: true},
		{Type: "text", Text: "project"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("system blocks = %#v, want %#v", blocks, want)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("system block %d = %#v, want %#v", i, blocks[i], want[i])
		}
	}
	if got := systemText(blocks); got != "identity\n\nproject" {
		t.Fatalf("flattened system = %q", got)
	}
}

// No declared break is one block: a provider that cannot place breakpoints
// sees exactly the prompt it saw before.
func TestProviderMessagesKeepsOneBlockWithoutBreaks(t *testing.T) {
	messages := []message.Message{
		{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "identity"}}},
		{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "project"}}},
	}
	_, blocks := providerMessages(messages)
	if len(blocks) != 1 || blocks[0].Text != "identity\n\nproject" || blocks[0].CacheBreak {
		t.Fatalf("system blocks = %#v", blocks)
	}
}
