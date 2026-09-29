package turn

// Sprint: #322; Story: #1160; Story-ID: 6ce0239a60a5

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/provider"
)

// A provider reports one response's usage in more than one event: the
// OpenAI-compatible adapter turns a final usage chunk into an input-token
// event (message_start) followed by an output-token event (message_delta),
// and Anthropic streams do the same natively. The collected response must
// carry both halves. When the second event replaced the first, every recorded
// usage held output tokens only, context.measure took a ~300-token context
// for a 10k one, and the budgets built on that measure were meaningless.
func TestCollectProviderMergesSplitUsage(t *testing.T) {
	stream := make(chan provider.Event, 4)
	stream <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 9000, CacheReadInput: 4000}}
	stream <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{OutputTokens: 250}}
	stream <- provider.Event{Type: provider.EventOutcome, Outcome: &provider.Outcome{Class: provider.OutcomeCompleted}}
	close(stream)

	response, _ := collectProvider(context.Background(), stream)
	usage, ok := response["usage"].(provider.Usage)
	if !ok {
		t.Fatalf("response usage = %#v, want provider.Usage", response["usage"])
	}
	want := provider.Usage{InputTokens: 9000, OutputTokens: 250, CacheReadInput: 4000}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

// A later event that restates a field (Anthropic's message_delta repeats the
// cumulative output count) updates it; a zero field never erases a value.
func TestCollectProviderUsageLaterNonZeroWins(t *testing.T) {
	stream := make(chan provider.Event, 3)
	stream <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 100, OutputTokens: 1}}
	stream <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{OutputTokens: 42}}
	close(stream)

	response, _ := collectProvider(context.Background(), stream)
	if got := response["usage"].(provider.Usage); got != (provider.Usage{InputTokens: 100, OutputTokens: 42}) {
		t.Fatalf("usage = %+v", got)
	}
}
