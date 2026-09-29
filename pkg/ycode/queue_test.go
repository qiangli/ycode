package ycode

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
)

func loadQueueHarness(t *testing.T) (*Harness, *stubProvider) {
	t.Helper()
	isolateHarnessStores(t)
	backend := newStubProvider(api.ProviderOpenAI)
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "ok"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: text}, {Type: "message_delta", Delta: stop}}
	}
	harness, err := Load(filepath.Join("..", "..", "examples", "agent.yaml"), WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = harness.Close() })
	return harness, backend
}

func lastUserText(request *api.Request) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		message := request.Messages[i]
		if message.Role != api.RoleUser {
			continue
		}
		var parts []string
		for _, block := range message.Content {
			parts = append(parts, block.Text)
		}
		return strings.Join(parts, "")
	}
	return ""
}

// The compiled loop drains [interrupt, steering] before every model call:
// a steer queued for the session reaches the model as a user message after
// the turn's own input, and the drain is a durable event.
func TestHarnessQueuedSteeringReachesTheModel(t *testing.T) {
	harness, backend := loadQueueHarness(t)
	if err := harness.Enqueue(QueueRequest{SessionID: "steer-session", QueueRef: "interactive", Class: "steering", Text: "STOP: a parallel lane owns this"}); err != nil {
		t.Fatal(err)
	}
	stream, err := harness.Run(context.Background(), RunRequest{SessionID: "steer-session", RunID: "steer-run", TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "tester", IdempotencyKey: "steer", Body: []byte(`{"request":"migrate m1..m6"}`)})
	if err != nil {
		t.Fatal(err)
	}
	drained := 0
	for item := range stream {
		if item.Type == "queue.drained" {
			var data struct {
				Count int `json:"count"`
			}
			_ = json.Unmarshal(item.Data, &data)
			drained += data.Count
		}
	}
	if drained != 1 {
		t.Fatalf("drained %d items, want the one steer", drained)
	}
	if got := lastUserText(backend.requests[0]); got != "STOP: a parallel lane owns this" {
		t.Fatalf("the model's last user message = %q, want the steer", got)
	}
	if left, err := harness.TakeQueued("steer-session", "interactive"); err != nil || len(left) != 0 {
		t.Fatalf("left = %#v, %v; the turn consumed the steer", left, err)
	}
}

// Items are per session, drained in the declared priority-fifo order; only
// declared classes are accepted; what no turn drained is taken back whole.
func TestHarnessQueueDisciplineAndTakeBack(t *testing.T) {
	harness, _ := loadQueueHarness(t)
	for _, item := range []QueueRequest{
		{SessionID: "a", QueueRef: "interactive", Class: "user", Text: "u1"},
		{SessionID: "a", QueueRef: "interactive", Class: "steering", Text: "s1"},
		{SessionID: "b", QueueRef: "interactive", Class: "steering", Text: "other session"},
		{SessionID: "a", QueueRef: "interactive", Class: "interrupt", Text: "i1"},
		{SessionID: "a", QueueRef: "interactive", Class: "steering", Text: "s2"},
		{SessionID: "a", QueueRef: "interactive", Class: "steering", Text: "dup", IdempotencyKey: "k"},
		{SessionID: "a", QueueRef: "interactive", Class: "steering", Text: "dup", IdempotencyKey: "k"},
	} {
		if err := harness.Enqueue(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := harness.Enqueue(QueueRequest{SessionID: "a", QueueRef: "interactive", Class: "gossip", Text: "x"}); err == nil {
		t.Fatal("an undeclared class was accepted")
	}
	if err := harness.Enqueue(QueueRequest{SessionID: "a", QueueRef: "nowhere", Class: "steering", Text: "x"}); err == nil {
		t.Fatal("an undeclared queue was accepted")
	}
	items, err := harness.queue.Drain(context.Background(), "a", "interactive", []string{"interrupt", "steering"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range items {
		got = append(got, item.Text)
	}
	if strings.Join(got, ",") != "i1,s1,s2,dup" {
		t.Fatalf("drained %q, want interrupt first then steering in arrival order, deduplicated", got)
	}
	left, err := harness.TakeQueued("a", "interactive")
	if err != nil || len(left) != 1 || left[0].Text != "u1" || left[0].Class != "user" {
		t.Fatalf("take back = %#v, %v", left, err)
	}
	other, _ := harness.TakeQueued("b", "interactive")
	if len(other) != 1 || other[0].Text != "other session" {
		t.Fatalf("session b = %#v", other)
	}
	events, err := event.Replay(harness.eventPath)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, taken := 0, 0
	for _, item := range events {
		switch item.Type {
		case "queue.enqueued":
			enqueued++
		case "queue.taken":
			taken++
		}
	}
	if enqueued != 6 || taken != 2 {
		t.Fatalf("events: enqueued=%d taken=%d, want 6 (one duplicate dropped) and 2", enqueued, taken)
	}
}

// The declared capacity bounds a queue; overflow reject-new refuses the item.
func TestHarnessQueueCapacityRejectsNew(t *testing.T) {
	harness, _ := loadQueueHarness(t)
	configured := harness.doc.Spec.Queues["interactive"]
	configured.Capacity = 2
	harness.doc.Spec.Queues["interactive"] = configured
	for i := 0; i < 2; i++ {
		if err := harness.Enqueue(QueueRequest{SessionID: "a", QueueRef: "interactive", Class: "steering", Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := harness.Enqueue(QueueRequest{SessionID: "b", QueueRef: "interactive", Class: "steering", Text: "x"}); err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("err = %v, want the queue full", err)
	}
}

// Settle returns once a session has no active run, and gives up with ctx.
func TestHarnessSettleWaitsForActiveRun(t *testing.T) {
	harness, _ := loadQueueHarness(t)
	done := make(chan struct{})
	harness.mu.Lock()
	harness.active[runKey("s", "r")] = &activeRun{done: done}
	harness.mu.Unlock()
	short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if harness.Settle(short, "s") {
		t.Fatal("settled while the run was active")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		harness.mu.Lock()
		delete(harness.active, runKey("s", "r"))
		harness.mu.Unlock()
		close(done)
	}()
	if !harness.Settle(context.Background(), "s") {
		t.Fatal("did not settle after the run ended")
	}
	if !harness.Settle(context.Background(), "other") {
		t.Fatal("a session without runs must settle at once")
	}
}
