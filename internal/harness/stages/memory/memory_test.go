package memory

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestMeasureAndReplayPayloads(t *testing.T) {
	engine, logPath, payloads := testEngine(t, "preserve-original", &fakeSummarizer{summary: "compact summary"})
	meta := testMeta()

	measurement, err := engine.Measure(context.Background(), meta, MeasureRequest{MemoryRef: "main", RouteRef: "main-route", SafetyMargin: 1, Messages: testMessages(2)})
	if err != nil || measurement.Tokens != 4 {
		t.Fatalf("measurement = %#v, %v", measurement, err)
	}
	if _, err := payloads.Get(measurement.PayloadRef); err != nil {
		t.Fatal(err)
	}

	events, err := event.Replay(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"context.measured"}
	var got []string
	for _, item := range events {
		got = append(got, item.Type)
		if strings.Contains(string(item.Data), "two words") {
			t.Fatalf("event embeds message bytes: %s", item.Data)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestCompactionUsesCompiledTriggerRoutePreservationAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		summarizer := &fakeSummarizer{summary: "summary"}
		engine, logPath, payloads := testEngine(t, "preserve-original", summarizer)
		messages := testMessages(5)
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: messages})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Triggered || result.Outcome != "compacted" || result.PreservedTokens != 4 || len(result.Messages) != 3 {
			t.Fatalf("result = %#v", result)
		}
		if summarizer.route != "main-route" || !strings.Contains(summarizer.system, "Handoff summary") || len(summarizer.messages) != 1 {
			t.Fatalf("summarizer route/system/messages = %q/%q/%d", summarizer.route, summarizer.system, len(summarizer.messages))
		}
		if result.Messages[0].Role != message.RoleUser || !strings.Contains(result.Messages[0].Content[0].Text, "compaction-summary") {
			t.Fatalf("summary message = %#v", result.Messages[0])
		}
		for _, ref := range []string{result.InputRef, result.SummaryRef, result.MessagesRef} {
			if _, err := payloads.Get(ref); err != nil {
				t.Fatalf("payload %s: %v", ref, err)
			}
		}
		events, err := event.Replay(logPath)
		if err != nil || len(events) != 1 || events[0].Type != "memory.compacted" {
			t.Fatalf("events = %#v, %v", events, err)
		}
	})

	t.Run("configured preserve original", func(t *testing.T) {
		engine, _, _ := testEngine(t, "preserve-original", &fakeSummarizer{err: errors.New("route down")})
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)})
		if err != nil || result.Outcome != "preserved-original" || len(result.Messages) != 5 {
			t.Fatalf("result = %#v, %v", result, err)
		}
	})

	t.Run("configured fail", func(t *testing.T) {
		engine, _, _ := testEngine(t, "fail", &fakeSummarizer{err: errors.New("route down")})
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)})
		if err == nil || result.Outcome != "failed" {
			t.Fatalf("result = %#v, %v", result, err)
		}
	})

	t.Run("previous summary uses update prompt", func(t *testing.T) {
		summarizer := &fakeSummarizer{summary: "merged"}
		engine, _, _ := testEngine(t, "preserve-original", summarizer)
		_, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5), PreviousSummary: "old summary"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(summarizer.system, "Updated handoff summary") || !strings.Contains(summarizer.messages[0].Content[0].Text, "PreviousSummary") {
			t.Fatalf("previous summary was not threaded: system=%q messages=%#v", summarizer.system, summarizer.messages)
		}
	})

	t.Run("fallback deterministic does not call route", func(t *testing.T) {
		summarizer := &fakeSummarizer{err: errors.New("route down")}
		engine, _, _ := testEngine(t, "fallback-deterministic", summarizer)
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)})
		if err != nil || result.Outcome != "fallback-deterministic" || !strings.Contains(result.Messages[0].Content[0].Text, "Deterministic excerpt summary") {
			t.Fatalf("result = %#v, %v", result, err)
		}
		if summarizer.calls != 1 {
			t.Fatalf("summarizer calls = %d", summarizer.calls)
		}
	})

	// Sprint 379 Story #51 (a87716307f79): a headless genie worker re-ran the
	// same `sed`/`cat`/`grep` reads after every compaction because the
	// deterministic fallback dropped which files had already been read. The
	// fallback summary must list them explicitly so the next turn does not
	// repeat the read.
	t.Run("fallback deterministic lists files already read", func(t *testing.T) {
		summarizer := &fakeSummarizer{err: errors.New("route down")}
		engine, _, _ := testEngine(t, "fallback-deterministic", summarizer)
		messages := []message.Message{
			{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: "call-1", Name: "bashy", Input: []byte(`{"script":"sed -n '1,50p' pkg/chat/chat.go"}`)}}},
			{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: "call-1", Content: "package chat"}}},
			{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: "call-2", Name: "bashy", Input: []byte(`{"script":"cat shell_shim_unix_test.go"}`)}}},
			{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: "call-2", Content: "package shell"}}},
		}
		messages = append(messages, testMessages(5)...)
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: messages})
		if err != nil || result.Outcome != "fallback-deterministic" {
			t.Fatalf("result = %#v, %v", result, err)
		}
		summary := result.Messages[0].Content[0].Text
		if !strings.Contains(summary, "Files already read") || !strings.Contains(summary, "pkg/chat/chat.go") || !strings.Contains(summary, "shell_shim_unix_test.go") {
			t.Fatalf("summary does not list the files already read: %s", summary)
		}
	})
}

func TestFilesReadExtractsReadOnlyCommandTargetsOnce(t *testing.T) {
	messages := []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, Name: "bashy", Input: []byte(`{"script":"sed -n '1,50p' pkg/chat/chat.go"}`)}}},
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, Name: "bashy", Input: []byte(`{"script":"cat shell_shim_unix_test.go && sed -n '1,10p' pkg/chat/chat.go"}`)}}},
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, Name: "bashy", Input: []byte(`{"script":"printf hello"}`)}}},
	}
	got := filesRead(messages)
	want := []string{"pkg/chat/chat.go", "shell_shim_unix_test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filesRead = %#v, want %#v", got, want)
	}
}

func TestMeasureUsesProviderUsageAndComputedBudget(t *testing.T) {
	engine, _, _ := testEngine(t, "preserve-original", &fakeSummarizer{summary: "x"})
	messages := testMessages(1)
	messages = append(messages, message.Message{Role: message.RoleAssistant, Usage: &message.TokenUsage{InputTokens: 10, CacheReadInput: 2, CacheCreationInput: 3, OutputTokens: 5}, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "ok"}}})
	messages = append(messages, testMessages(1)...)
	measurement, err := engine.Measure(context.Background(), testMeta(), MeasureRequest{MemoryRef: "main", RouteRef: "main-route", SafetyMargin: 1.5, Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Tokens != 23 || measurement.ContextBudget != 84 || measurement.TruncateBudget != 76 || measurement.Measured.Source != "provider" || measurement.Measured.ProviderTokens != 20 || measurement.Measured.EstimatedTail != 2 {
		t.Fatalf("measurement = %#v", measurement)
	}
}

// Sprint 379 Story #53 (c88f66baf976): a live glm-5.3 session (selected via
// `ycode model use`, a declared fallback attempt in the route) measured
// context against route.Attempts[0]'s model instead of the one actually
// serving the turn, so a model with a far larger window still got the
// default attempt's tiny budget and started truncating tool results after a
// handful of turns. ModelRef must steer the measurement to the session's
// chosen attempt, the same way routeProvider already steers inference.
func TestMeasureUsesSessionSelectedModelNotRouteDefault(t *testing.T) {
	doc := testDocument("preserve-original")
	route := doc.Spec.Routes["main-route"]
	route.Attempts = append(route.Attempts, spec.RouteAttempt{ModelRef: "big-model", TimeoutMS: 100})
	doc.Spec.Routes["main-route"] = route
	doc.Spec.Models["big-model"] = spec.Model{Limits: spec.ModelLimits{ContextTokens: 100000, MaxOutputTokens: 20}}
	engine, _, _ := testEngineWithDocument(t, doc, &fakeSummarizer{summary: "x"})

	measurement, err := engine.Measure(context.Background(), testMeta(), MeasureRequest{MemoryRef: "main", RouteRef: "main-route", ModelRef: "big-model", SafetyMargin: 1, Messages: nil})
	if err != nil {
		t.Fatal(err)
	}
	// 100000 - 8 (route's output reserve, capped by big-model's own 20) - 8 (compaction reserveTokens).
	if measurement.ContextBudget != 99984 {
		t.Fatalf("contextBudget = %d, want the selected model's window (99984), not the route default's", measurement.ContextBudget)
	}

	if _, err := engine.Measure(context.Background(), testMeta(), MeasureRequest{MemoryRef: "main", RouteRef: "main-route", ModelRef: "not-in-route", SafetyMargin: 1, Messages: nil}); err == nil {
		t.Fatal("a model ref outside the declared route's attempts was accepted")
	}
}

func TestPreserveBoundaryKeepsToolPairsTogether(t *testing.T) {
	engine, _, _ := testEngine(t, "preserve-original", &fakeSummarizer{summary: "summary"})
	messages := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "older turn"}}},
		{Role: message.RoleAssistant, Content: []message.ContentBlock{{Type: message.ContentTypeToolUse, ID: "call", Name: "bashy"}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: "call", Content: "tool output"}}},
	}
	result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) < 3 || !hasToolUse(result.Messages[len(result.Messages)-2]) || !hasToolResult(result.Messages[len(result.Messages)-1]) {
		t.Fatalf("tool pair separated: %#v", result.Messages)
	}
}

func TestCompactionMissingPromptSourceFailsClosed(t *testing.T) {
	doc := testDocument("preserve-original")
	mem := doc.Spec.Memories["main"]
	mem.Compaction.PromptSourceRef = ""
	doc.Spec.Memories["main"] = mem
	engine, _, _ := testEngineWithDocument(t, doc, &fakeSummarizer{summary: "x"})
	if _, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)}); err == nil {
		t.Fatal("missing prompt source was accepted")
	}
}

func TestStagesRejectNonBashyKBProvider(t *testing.T) {
	doc := testDocument("preserve-original")
	memoryConfig := doc.Spec.Memories["main"]
	memoryConfig.Provider = "memex"
	doc.Spec.Memories["main"] = memoryConfig
	bad, _, _ := testEngineWithDocument(t, doc, &fakeSummarizer{})
	if _, err := bad.Measure(context.Background(), testMeta(), MeasureRequest{MemoryRef: "main", RouteRef: "main-route", SafetyMargin: 1, Messages: testMessages(1)}); err == nil {
		t.Fatal("removed memex provider was accepted")
	}
	if _, err := bad.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(1)}); err == nil {
		t.Fatal("removed memex provider was accepted by compaction")
	}
	// Compaction scheduling belongs to the compiled context.measure -> switch
	// graph, not to this mechanism.
}

func testDocument(onFailure string) *spec.Document {
	return &spec.Document{Spec: spec.Spec{
		Sources: map[string]spec.Source{
			"compact": {Resolved: "[compaction-summary] Handoff summary:"},
			"update":  {Resolved: "[compaction-summary] Updated handoff summary:"},
		},
		Memories: map[string]spec.Memory{"main": {Provider: "bashy-kb", Recall: spec.RecallPolicy{Rings: []string{"agent", "repo"}, Forms: []string{"note", "page"}, MaxItems: 3, MaxTokens: 3}, Write: spec.WritePolicy{EveryTurns: 1}, Compaction: spec.CompactionPolicy{PreserveRecentTokens: 4, PreserveUserMessagesTokens: 0, ReserveTokens: 8, RouteRef: "main-route", PromptSourceRef: "compact", UpdatePromptSourceRef: "update", OnFailure: onFailure}}},
		Routes:   map[string]spec.Route{"main-route": {Attempts: []spec.RouteAttempt{{ModelRef: "model", TimeoutMS: 100}}, Budget: spec.TokenBudget{MaxOutputTokens: 8}}},
		Models:   map[string]spec.Model{"model": {Limits: spec.ModelLimits{ContextTokens: 100, MaxOutputTokens: 20}}},
	}}
}

func testEngine(t *testing.T, onFailure string, summarizer *fakeSummarizer) (*Engine, string, *event.PayloadStore) {
	t.Helper()
	return testEngineWithDocument(t, testDocument(onFailure), summarizer)
}

func testEngineWithDocument(t *testing.T, doc *spec.Document, summarizer Summarizer) (*Engine, string, *event.PayloadStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BASHY_KB_DIR", filepath.Join(dir, "kb"))
	t.Setenv("BASHY_HOME", filepath.Join(dir, "bashy-home"))
	t.Setenv("BASHY_SKILLS_DIR", filepath.Join(dir, "skills"))
	t.Setenv("YCODE_DATA_DIR", filepath.Join(dir, "ycode-data"))
	logPath := filepath.Join(dir, "events.jsonl")
	events, err := event.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Document: doc, Events: events, Payloads: payloads, Tokens: wordTokens{}, Summary: summarizer})
	if err != nil {
		t.Fatal(err)
	}
	return engine, logPath, payloads
}

func testMeta() Meta {
	return Meta{SessionID: "s", RunID: "r", StageID: "memory", ConfigDigest: "sha256:c"}
}

func testMessages(count int) []message.Message {
	result := make([]message.Message, count)
	for i := range result {
		result[i] = message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "two words"}}}
	}
	return result
}

type wordTokens struct{}

func (wordTokens) CountMessages(messages []message.Message) (int, error) {
	total := 0
	for _, message := range messages {
		for _, block := range message.Content {
			total += len(strings.Fields(block.Text + " " + block.Content + " " + string(block.Input)))
		}
	}
	return total, nil
}

type fakeSummarizer struct {
	mu       sync.Mutex
	summary  string
	err      error
	route    string
	system   string
	messages []message.Message
	calls    int
}

func (f *fakeSummarizer) Summarize(_ context.Context, route, system string, messages []message.Message, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.route = route
	f.system = system
	f.messages = cloneMessages(messages)
	return f.summary, f.err
}

func TestMeasureReservesOneResponseNotTheWholeRouteBudget(t *testing.T) {
	doc := testDocument("fallback-deterministic")
	route := doc.Spec.Routes["main-route"]
	route.Budget.MaxOutputTokens = 1000 // a whole-run allowance, far above one response
	doc.Spec.Routes["main-route"] = route
	engine, _, _ := testEngineWithDocument(t, doc, &fakeSummarizer{})
	measurement, err := engine.Measure(context.Background(), testMeta(), MeasureRequest{MemoryRef: "main", RouteRef: "main-route", SafetyMargin: 1, Messages: nil})
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	// 100 context - min(1000 route, 20 model) output - 8 reserve
	if measurement.ContextBudget != 72 {
		t.Fatalf("ContextBudget = %d, want 72", measurement.ContextBudget)
	}
}
