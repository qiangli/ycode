package turn

// Sprint: #387; Story: #27b3dfdf; Story-ID: 27b3dfdf7e76

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/provider"
)

// Provider deltas reach a live observer as they arrive, coalesced by line,
// and concatenate to exactly the canonical folded text.
func TestCollectProviderForwardsDeltas(t *testing.T) {
	stream := make(chan provider.Event, 8)
	for _, chunk := range []string{"Hel", "lo\n", "wor", "ld"} {
		stream <- provider.Event{Type: provider.EventTextDelta, Text: chunk}
	}
	stream <- provider.Event{Type: provider.EventThinkingDelta, Text: "hmm"}
	stream <- provider.Event{Type: provider.EventOutcome, Outcome: &provider.Outcome{Class: provider.OutcomeCompleted}}
	close(stream)

	var texts, thinking []string
	response, _ := collectProvider(context.Background(), stream, func(channel, chunk string) error {
		if channel == "text" {
			texts = append(texts, chunk)
		} else {
			thinking = append(thinking, chunk)
		}
		return nil
	})
	if got := strings.Join(texts, ""); got != response["text"] || got != "Hello\nworld" {
		t.Fatalf("deltas %q joined %q, response %q", texts, got, response["text"])
	}
	if len(texts) != 2 || texts[0] != "Hello\n" {
		t.Fatalf("deltas = %q, want line-coalesced [\"Hello\\n\" \"world\"]", texts)
	}
	if strings.Join(thinking, "") != "hmm" {
		t.Fatalf("thinking deltas = %q", thinking)
	}
}

// A delta the observer cannot record fails the attempt with the observer's
// error instead of reporting a completed response.
func TestCollectProviderPropagatesDeltaSinkError(t *testing.T) {
	stream := make(chan provider.Event, 4)
	stream <- provider.Event{Type: provider.EventTextDelta, Text: "line\n"}
	stream <- provider.Event{Type: provider.EventTextDelta, Text: "more"}
	stream <- provider.Event{Type: provider.EventOutcome, Outcome: &provider.Outcome{Class: provider.OutcomeCompleted}}
	close(stream)
	_, outcome := collectProvider(context.Background(), stream, func(string, string) error {
		return errors.New("journal full")
	})
	if outcome.Class != provider.OutcomeProtocolError || !strings.Contains(outcome.Error, "journal full") {
		t.Fatalf("outcome = %+v, want protocol error carrying the sink error", outcome)
	}
}
