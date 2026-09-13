package ycode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

func TestHarnessLoadValidateRunStreamsDurableEvents(t *testing.T) {
	isolateHarnessStores(t)
	backend := newStubProvider(api.ProviderOpenAI)
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "public yaml result"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: text}, {Type: "message_delta", Delta: stop}}
	}
	path := filepath.Join("..", "..", "examples", "agent.yaml")
	if err := Validate(path); err != nil {
		t.Fatal(err)
	}
	harness, err := Load(path, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	// The bashy-kb provider replaced memex: Load must not open or create a
	// memex store under the control root.
	if _, err := os.Stat(filepath.Join(harness.control, "memex")); !os.IsNotExist(err) {
		t.Fatalf("Load created a memex store under the control root: %v", err)
	}
	stream, err := harness.Run(context.Background(), RunRequest{SessionID: "public-session", RunID: "public-run", TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "tester", IdempotencyKey: "once", Body: []byte(`{"request":"run from yaml"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	var outputPayload string
	var lastSequence uint64
	for item := range stream {
		lastSequence = item.Sequence
		if item.PreviousDigest != previous {
			t.Fatalf("broken streamed hash chain at sequence %d", item.Sequence)
		}
		previous = item.Digest
		if item.ConfigDigest != harness.doc.ConfigDigest {
			t.Fatalf("config digest = %q", item.ConfigDigest)
		}
		if item.Type == "output.emitted" {
			var data struct {
				Deliveries []struct {
					PayloadRef string `json:"payload_ref"`
				} `json:"deliveries"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				t.Fatal(err)
			}
			if len(data.Deliveries) != 1 {
				t.Fatalf("deliveries = %#v", data.Deliveries)
			}
			outputPayload = data.Deliveries[0].PayloadRef
		}
	}
	if outputPayload == "" {
		t.Fatal("stream omitted output.emitted")
	}
	content, err := harness.Payload(outputPayload)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "public yaml result" {
		t.Fatalf("payload = %q", content)
	}
	request := backend.lastReq
	if request == nil || len(request.Tools) != 1 || request.Tools[0].Name != "bashy" {
		t.Fatalf("provider tools = %#v", request)
	}
	fork, err := harness.Fork(context.Background(), ForkRequest{ParentSessionID: "public-session", SessionID: "public-child", RunID: "fork-public-child", AtSequence: lastSequence})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]Event, 0, 1)
	for item := range fork {
		items = append(items, item)
	}
	if len(items) != 1 || items[0].Type != "session.forked" || items[0].SessionID != "public-child" || items[0].CausationID != previous {
		t.Fatalf("fork events = %#v", items)
	}
	if _, err := os.Stat(harness.boundaryPath("public-child", "fork-public-child")); err != nil {
		t.Fatalf("fork checkpoint: %v", err)
	}
}

func TestHarnessResumeContinuesSuspendedGraphWithoutRestart(t *testing.T) {
	isolateHarnessStores(t)
	var calls atomic.Int32
	backend := newStubProvider(api.ProviderOpenAI)
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		if calls.Add(1) == 1 {
			stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
			return []*api.StreamEvent{
				// An exact workspace overwrite matches the compiled destructive
				// rule, so the suspended-graph review path stays exercised now
				// that pure builtin evidence no longer blocks the allow rule.
				{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "review-call", Name: "bashy", Input: json.RawMessage(`{"script":"printf harmless > review.txt"}`)}},
				{Type: "content_block_stop"}, {Type: "message_delta", Delta: stop},
			}
		}
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "resumed answer"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: text}, {Type: "message_delta", Delta: stop}}
	}
	canonical := filepath.Join("..", "..", "examples", "agent.yaml")
	raw, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), raw...)
	raw = bytes.Replace(raw, []byte("        - id: default\n          match: {always: true}\n          decision: deny"), []byte("        - id: default\n          match: {always: true}\n          decision: ask"), 1)
	if bytes.Equal(raw, original) {
		t.Fatal("test policy fixture was not rewritten")
	}
	fixture, err := os.CreateTemp(filepath.Dir(canonical), ".resume-agent-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := fixture.Name()
	defer os.Remove(fixturePath)
	if _, err := fixture.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	harness, err := Load(fixturePath, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := harness.Run(ctx, RunRequest{SessionID: "resume-session", RunID: "resume-run", TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "tester", IdempotencyKey: "resume-once", HumanAvailable: true, Body: []byte(`{"request":"exercise approval"}`)})
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	completed := false
	admitted, assembled := 0, 0
	for item := range stream {
		if item.Type == "input.admitted" {
			admitted++
		}
		if item.Type == "prompt.assembled" {
			assembled++
		}
		if item.Type == "hitl.waiting" {
			var pending struct {
				DecisionID   string `json:"decision_id"`
				Version      uint64 `json:"version"`
				ReviewDigest string `json:"review_digest"`
				ReportDigest string `json:"report_digest"`
			}
			if err := json.Unmarshal(item.Data, &pending); err != nil {
				t.Fatal(err)
			}
			resumeStream, err := harness.Resume(ctx, ResumeRequest{SessionID: "resume-session", RunID: "resume-run", DecisionID: pending.DecisionID, ExpectedVersion: pending.Version, ReviewDigest: pending.ReviewDigest, ReportDigest: pending.ReportDigest, Action: "reject", Actor: "tester"})
			if err != nil {
				t.Fatal(err)
			}
			resumed = true
			go func() {
				for range resumeStream {
				}
			}()
		}
		if item.Type == "output.emitted" {
			completed = true
		}
	}
	if !resumed || !completed {
		t.Fatalf("resumed=%v completed=%v", resumed, completed)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want tool step plus resumed final step", calls.Load())
	}
	if admitted != 1 || assembled != 1 {
		t.Fatalf("resume reran entry stages: admitted=%d assembled=%d", admitted, assembled)
	}
}

func TestHarnessCompactsBeforeContextBudgetAndRetriesOverflowOnce(t *testing.T) {
	isolateHarnessStores(t)
	var calls atomic.Int32
	backend := newStubProvider(api.ProviderOpenAI)
	backend.streamFunc = func(request *api.Request) []*api.StreamEvent {
		call := calls.Add(1)
		textValue := "final after retry"
		if strings.Contains(request.System, "Handoff summary") || strings.Contains(request.System, "Updated handoff summary") {
			textValue = "goal: keep working\nnext steps: answer"
		} else if call == 2 {
			return nil
		}
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": textValue})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: text}, {Type: "message_delta", Delta: stop}}
	}
	backend.errFunc = func(request *api.Request) error {
		if !strings.Contains(request.System, "Handoff summary") && calls.Load() == 2 {
			return errors.New("maximum context length is 40 tokens")
		}
		return nil
	}
	fixture := smallContextFixture(t)
	harness, err := Load(fixture, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	stream, err := harness.Run(context.Background(), RunRequest{SessionID: "compact-session", RunID: "compact-run", TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "tester", IdempotencyKey: "compact-once", Body: []byte(`{"request":"please do enough work to need compaction"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var compacted int
	var overflowCompleted int
	var retryRequestStartsWithSummary bool
	for item := range stream {
		switch item.Type {
		case "memory.compacted":
			compacted++
		case "llm.completed":
			var data struct {
				Outcome string `json:"outcome"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Outcome == "context-overflow" {
				overflowCompleted++
			}
		case "llm.requested":
			var data struct {
				PayloadRef string `json:"payload_ref"`
			}
			if err := json.Unmarshal(item.Data, &data); err != nil {
				t.Fatal(err)
			}
			payload, err := harness.Payload(data.PayloadRef)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(payload, []byte(`"role":"user"`)) && bytes.Contains(payload, []byte("compaction-summary")) {
				retryRequestStartsWithSummary = true
			}
		}
	}
	if compacted == 0 {
		t.Fatal("memory.compacted did not fire")
	}
	if overflowCompleted != 1 {
		t.Fatalf("context-overflow completions = %d, want exactly one", overflowCompleted)
	}
	if !retryRequestStartsWithSummary {
		t.Fatal("no llm.requested payload began from a tagged compaction summary")
	}
}

func isolateHarnessStores(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("BASHY_KB_DIR", filepath.Join(dir, "kb"))
	t.Setenv("BASHY_HOME", filepath.Join(dir, "bashy-home"))
	t.Setenv("BASHY_SKILLS_DIR", filepath.Join(dir, "skills"))
	t.Setenv("YCODE_DATA_DIR", filepath.Join(dir, "ycode-data"))
}

func smallContextFixture(t *testing.T) string {
	t.Helper()
	canonical := filepath.Join("..", "..", "examples", "agent.yaml")
	raw, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	replacements := map[string]string{
		"contextTokens: 128000":            "contextTokens: 40",
		"maxOutputTokens: 100000":          "maxOutputTokens: 10",
		"maxOutputTokens: 20000":           "maxOutputTokens: 10",
		"preserveRecentTokens: 24000":      "preserveRecentTokens: 6",
		"preserveUserMessagesTokens: 4000": "preserveUserMessagesTokens: 6",
		"reserveTokens: 8000":              "reserveTokens: 4",
	}
	for old, replacement := range replacements {
		raw = bytes.Replace(raw, []byte(old), []byte(replacement), 1)
	}
	fixture, err := os.CreateTemp(filepath.Dir(canonical), ".compact-agent-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err := fixture.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
