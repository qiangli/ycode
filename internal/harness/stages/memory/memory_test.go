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
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

func TestRecallWriteMeasureAndReplayPayloads(t *testing.T) {
	t.Parallel()
	facade := &fakeFacade{hits: []RecallHit{
		{Memory: &memexmemory.Memory{Name: "first", Content: "two words", Scope: memexmemory.ScopeProject}, Score: .9, Source: "vector"},
		{Memory: &memexmemory.Memory{Name: "second", Content: "also two", Scope: memexmemory.ScopeProject}, Score: .8, Source: "keyword"},
	}}
	engine, logPath, payloads := testEngine(t, "preserve-original", facade, &fakeSummarizer{summary: "compact summary"})
	meta := testMeta()

	recalled, err := engine.Recall(context.Background(), meta, "main", "where bug", "coder")
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Items) != 1 || recalled.Items[0].Name != "first" || recalled.Tokens != 2 {
		t.Fatalf("recall = %#v", recalled)
	}
	if facade.lastQuery.Ranking != "hybrid" || !reflect.DeepEqual(facade.lastQuery.Scopes, []string{"workspace", "agent"}) || facade.lastQuery.MaxResults != 3 {
		t.Fatalf("facade query = %#v", facade.lastQuery)
	}
	if _, err := payloads.Get(recalled.QueryRef); err != nil {
		t.Fatal(err)
	}
	if _, err := payloads.Get(recalled.Items[0].PayloadRef); err != nil {
		t.Fatal(err)
	}

	written, err := engine.Write(context.Background(), meta, "main", []*memexmemory.Memory{{Name: "decision", Content: "use yaml", Scope: memexmemory.ScopeProject}})
	if err != nil {
		t.Fatal(err)
	}
	if len(written.PayloadRefs) != 1 || len(facade.writes) != 1 {
		t.Fatalf("write = %#v, facade writes=%d", written, len(facade.writes))
	}

	measurement, err := engine.Measure(context.Background(), meta, testMessages(2))
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
	want := []string{"memory.recalled", "memory.write.requested", "memory.write.completed", "context.measured"}
	var got []string
	for _, item := range events {
		got = append(got, item.Type)
		if strings.Contains(string(item.Data), "two words") || strings.Contains(string(item.Data), "use yaml") {
			t.Fatalf("event embeds memory bytes: %s", item.Data)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestCompactionUsesCompiledTriggerRoutePreservationAndFailure(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		summarizer := &fakeSummarizer{summary: "summary"}
		engine, logPath, payloads := testEngine(t, "preserve-original", &fakeFacade{}, summarizer)
		messages := testMessages(5)
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: messages})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Triggered || result.Outcome != "compacted" || result.PreservedTokens != 4 || len(result.Messages) != 3 {
			t.Fatalf("result = %#v", result)
		}
		if summarizer.route != "main-route" || len(summarizer.messages) != 3 {
			t.Fatalf("summarizer route/messages = %q/%d", summarizer.route, len(summarizer.messages))
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
		engine, _, _ := testEngine(t, "preserve-original", &fakeFacade{}, &fakeSummarizer{err: errors.New("route down")})
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)})
		if err != nil || result.Outcome != "preserved-original" || len(result.Messages) != 5 {
			t.Fatalf("result = %#v, %v", result, err)
		}
	})

	t.Run("configured fail", func(t *testing.T) {
		engine, _, _ := testEngine(t, "fail", &fakeFacade{}, &fakeSummarizer{err: errors.New("route down")})
		result, err := engine.Compact(context.Background(), testMeta(), CompactionRequest{MemoryRef: "main", Messages: testMessages(5)})
		if err == nil || result.Outcome != "failed" {
			t.Fatalf("result = %#v, %v", result, err)
		}
	})
}

func TestStagesRejectImplicitConfiguration(t *testing.T) {
	t.Parallel()
	engine, _, _ := testEngine(t, "preserve-original", &fakeFacade{}, &fakeSummarizer{summary: "x"})
	if _, err := engine.Recall(context.Background(), testMeta(), "main", "q", ""); err != nil {
		t.Fatalf("declared recall unexpectedly failed: %v", err)
	}
	doc := testDocument("preserve-original")
	memoryConfig := doc.Spec.Memories["main"]
	memoryConfig.Recall.Ranking = "ambient-default"
	doc.Spec.Memories["main"] = memoryConfig
	bad, _, _ := testEngineWithDocument(t, doc, &fakeFacade{}, &fakeSummarizer{})
	if _, err := bad.Recall(context.Background(), testMeta(), "main", "q", ""); err == nil {
		t.Fatal("undeclared ranking fallback was accepted")
	}
	// Compaction scheduling belongs to the compiled context.measure → switch
	// graph, not to this mechanism.
}

func testDocument(onFailure string) *spec.Document {
	return &spec.Document{Spec: spec.Spec{
		Memories: map[string]spec.Memory{"main": {Provider: "memex", Recall: spec.RecallPolicy{Scopes: []string{"workspace", "agent"}, Ranking: "hybrid", MaxItems: 3, MaxTokens: 3}, Write: spec.WritePolicy{MaxItems: 2, MaxBytes: 4096}, Compaction: spec.CompactionPolicy{PreserveRecentTokens: 4, RouteRef: "main-route", OnFailure: onFailure}}},
		Routes:   map[string]spec.Route{"main-route": {Attempts: []spec.RouteAttempt{{ModelRef: "model", TimeoutMS: 100}}}},
	}}
}

func testEngine(t *testing.T, onFailure string, facade *fakeFacade, summarizer *fakeSummarizer) (*Engine, string, *event.PayloadStore) {
	t.Helper()
	return testEngineWithDocument(t, testDocument(onFailure), facade, summarizer)
}

func testEngineWithDocument(t *testing.T, doc *spec.Document, facade Facade, summarizer Summarizer) (*Engine, string, *event.PayloadStore) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	events, err := event.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Document: doc, Events: events, Payloads: payloads, Facade: facade, Tokens: wordTokens{}, Summary: summarizer})
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

type fakeFacade struct {
	mu        sync.Mutex
	hits      []RecallHit
	lastQuery RecallQuery
	writes    []*memexmemory.Memory
}

func (f *fakeFacade) Recall(_ context.Context, query RecallQuery) ([]RecallHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	return append([]RecallHit(nil), f.hits...), nil
}

func (f *fakeFacade) Write(_ context.Context, memory *memexmemory.Memory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, cloneMemory(memory))
	return nil
}

type fakeSummarizer struct {
	mu       sync.Mutex
	summary  string
	err      error
	route    string
	messages []message.Message
}

func (f *fakeSummarizer) Summarize(_ context.Context, route string, messages []message.Message, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.route = route
	f.messages = cloneMessages(messages)
	return f.summary, f.err
}
