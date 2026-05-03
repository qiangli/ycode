package conversation

import (
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

func TestCostTracker_UnlimitedBudget(t *testing.T) {
	ct := NewCostTracker(0) // unlimited

	for range 100 {
		ct.RecordCall(api.Usage{InputTokens: 100, OutputTokens: 50})
	}

	if ct.BudgetExceeded() {
		t.Error("unlimited budget should never be exceeded")
	}
	if err := ct.BudgetError(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	snap := ct.Snapshot()
	if snap.BudgetRemaining() != -1 {
		t.Errorf("unlimited budget remaining = %d, want -1", snap.BudgetRemaining())
	}
}

func TestCostTracker_BudgetEnforcement(t *testing.T) {
	ct := NewCostTracker(3)

	ct.RecordCall(api.Usage{InputTokens: 100, OutputTokens: 50})
	if ct.BudgetExceeded() {
		t.Error("1/3: should not be exceeded")
	}

	ct.RecordCall(api.Usage{InputTokens: 200, OutputTokens: 100})
	if ct.BudgetExceeded() {
		t.Error("2/3: should not be exceeded")
	}

	ct.RecordCall(api.Usage{InputTokens: 150, OutputTokens: 75})
	if !ct.BudgetExceeded() {
		t.Error("3/3: should be exceeded")
	}

	err := ct.BudgetError()
	if err == nil {
		t.Fatal("expected budget error")
	}

	snap := ct.Snapshot()
	if snap.LLMCallCount != 3 {
		t.Errorf("call count = %d, want 3", snap.LLMCallCount)
	}
	if snap.TotalInputTokens != 450 {
		t.Errorf("input tokens = %d, want 450", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 225 {
		t.Errorf("output tokens = %d, want 225", snap.TotalOutputTokens)
	}
	if snap.TotalTokens() != 675 {
		t.Errorf("total tokens = %d, want 675", snap.TotalTokens())
	}
	if snap.BudgetRemaining() != 0 {
		t.Errorf("budget remaining = %d, want 0", snap.BudgetRemaining())
	}
}

func TestCostTracker_Reset(t *testing.T) {
	ct := NewCostTracker(2)
	ct.RecordCall(api.Usage{InputTokens: 100})
	ct.RecordCall(api.Usage{InputTokens: 100})

	if !ct.BudgetExceeded() {
		t.Error("should be exceeded before reset")
	}

	ct.Reset()

	if ct.BudgetExceeded() {
		t.Error("should not be exceeded after reset")
	}
	snap := ct.Snapshot()
	if snap.LLMCallCount != 0 {
		t.Errorf("after reset: call count = %d, want 0", snap.LLMCallCount)
	}
}

func TestCostTracker_CacheTracking(t *testing.T) {
	ct := NewCostTracker(0)
	ct.RecordCall(api.Usage{
		InputTokens:        100,
		OutputTokens:       50,
		CacheReadInput:     80,
		CacheCreationInput: 20,
	})

	snap := ct.Snapshot()
	if snap.TotalCacheRead != 80 {
		t.Errorf("cache read = %d, want 80", snap.TotalCacheRead)
	}
	if snap.TotalCacheCreate != 20 {
		t.Errorf("cache create = %d, want 20", snap.TotalCacheCreate)
	}
}

func TestCostTracker_PromptTokensCompat(t *testing.T) {
	ct := NewCostTracker(0)
	// OpenAI-style tokens.
	ct.RecordCall(api.Usage{PromptTokens: 100, CompletionTokens: 50})

	snap := ct.Snapshot()
	if snap.TotalInputTokens != 100 {
		t.Errorf("input tokens (from PromptTokens) = %d, want 100", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 50 {
		t.Errorf("output tokens (from CompletionTokens) = %d, want 50", snap.TotalOutputTokens)
	}
}
