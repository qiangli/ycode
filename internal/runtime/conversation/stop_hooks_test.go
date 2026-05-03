package conversation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/session"
)

func TestRunStopHooks_MemoryExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("async test")
	}

	state := NewStopHooksState()
	cfg := DefaultStopHooksConfig()
	cfg.MemoryExtractionInterval = 1 // Extract every turn.
	cfg.DreamEnabled = false

	var extractCalled atomic.Int32
	extractFn := func(_ context.Context, _ []session.ConversationMessage) {
		extractCalled.Add(1)
	}

	messages := []session.ConversationMessage{
		{Role: session.RoleUser, Content: []session.ContentBlock{{Type: session.ContentTypeText, Text: "test"}}},
	}

	RunStopHooks(context.Background(), state, cfg, messages, extractFn, nil)

	// Give async goroutine time to run.
	time.Sleep(100 * time.Millisecond)

	if extractCalled.Load() != 1 {
		t.Errorf("expected 1 extraction call, got %d", extractCalled.Load())
	}
}

func TestRunStopHooks_ExtractionInterval(t *testing.T) {
	state := NewStopHooksState()
	cfg := DefaultStopHooksConfig()
	cfg.MemoryExtractionInterval = 3
	cfg.DreamEnabled = false

	var extractCalled atomic.Int32
	extractFn := func(_ context.Context, _ []session.ConversationMessage) {
		extractCalled.Add(1)
	}

	messages := []session.ConversationMessage{}

	// Turn 1: should extract (0 + 3 = turn 3 threshold, but first time gap is 0-0 >= 3? No).
	// Actually: turnCount=1, lastExtractionAt=0, 1-0 >= 3? No.
	RunStopHooks(context.Background(), state, cfg, messages, extractFn, nil)
	time.Sleep(50 * time.Millisecond)

	// Turn 2.
	RunStopHooks(context.Background(), state, cfg, messages, extractFn, nil)
	time.Sleep(50 * time.Millisecond)

	// Turn 3: 3-0 >= 3 = YES.
	RunStopHooks(context.Background(), state, cfg, messages, extractFn, nil)
	time.Sleep(100 * time.Millisecond)

	if extractCalled.Load() != 1 {
		t.Errorf("expected 1 extraction (at turn 3), got %d", extractCalled.Load())
	}
}

func TestRunStopHooks_DreamGating(t *testing.T) {
	state := NewStopHooksState()
	cfg := DefaultStopHooksConfig()
	cfg.MemoryExtractionEnabled = false
	cfg.DreamEnabled = true
	cfg.DreamMinHours = 0    // No time gate.
	cfg.DreamMinSessions = 2 // Need 2 sessions.

	var dreamCalled atomic.Int32
	dreamFn := func(_ context.Context) {
		dreamCalled.Add(1)
	}

	// Turn 1: sessionsSinceDream=1, < 2 required.
	RunStopHooks(context.Background(), state, cfg, nil, nil, dreamFn)
	time.Sleep(50 * time.Millisecond)
	if dreamCalled.Load() != 0 {
		t.Error("dream should not trigger before minSessions")
	}

	// Turn 2: sessionsSinceDream=2, >= 2 required.
	RunStopHooks(context.Background(), state, cfg, nil, nil, dreamFn)
	time.Sleep(100 * time.Millisecond)
	if dreamCalled.Load() != 1 {
		t.Errorf("expected dream at turn 2, got %d calls", dreamCalled.Load())
	}
}

func TestCacheSafeParams(t *testing.T) {
	params := ComputeCacheSafeParams("system prompt", "tools json", "claude-3", "user ctx")

	if params.Model != "claude-3" {
		t.Errorf("expected model 'claude-3', got %s", params.Model)
	}
	if params.SystemPromptHash == "" {
		t.Error("system prompt hash should not be empty")
	}
	if params.ToolsHash == "" {
		t.Error("tools hash should not be empty")
	}
	if params.SnapshotAt.IsZero() {
		t.Error("snapshot time should be set")
	}
}

func TestStopHooksState_CacheParams(t *testing.T) {
	state := NewStopHooksState()

	if state.GetCacheParams() != nil {
		t.Error("initial cache params should be nil")
	}

	params := &CacheSafeParams{Model: "test"}
	state.UpdateCacheParams(params)

	got := state.GetCacheParams()
	if got == nil || got.Model != "test" {
		t.Error("should return stored params")
	}
}
