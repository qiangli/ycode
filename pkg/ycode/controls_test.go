package ycode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"gopkg.in/yaml.v3"
)

func controlRequest(session, run string) RunRequest {
	return RunRequest{SessionID: session, RunID: run, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "tester", IdempotencyKey: run, Body: []byte(`{"request":"make a plan"}`)}
}

func controlDrain(t *testing.T, stream <-chan Event) []Event {
	t.Helper()
	var events []Event
	for item := range stream {
		events = append(events, item)
		if item.Type == "turn.failed" {
			t.Fatalf("turn failed: %s", item.Data)
		}
	}
	return events
}

func TestSessionModelDurableIsolatedValidatedRoute(t *testing.T) {
	isolateHarnessStores(t)
	source := filepath.Join("..", "..", "examples", "agent.yaml")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	models := spec["models"].(map[string]any)
	primary := models["primary"].(map[string]any)
	alternate := map[string]any{}
	for k, v := range primary {
		alternate[k] = v
	}
	alternate["id"] = "alternate-provider-id"
	models["alternate"] = alternate
	route := spec["routes"].(map[string]any)["main"].(map[string]any)
	attempts := route["attempts"].([]any)
	attempt := map[string]any{}
	for k, v := range attempts[0].(map[string]any) {
		attempt[k] = v
	}
	attempt["modelRef"] = "alternate"
	route["attempts"] = append(attempts, attempt)
	raw, err = yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	backend := newStubProvider(api.ProviderOpenAI)
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: json.RawMessage(`{"type":"text_delta","text":"answer"}`)}}
	}
	h, err := LoadSource(source, raw, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(h.doc)
	if err := h.SetSessionModel("selected", "alternate"); err != nil {
		t.Fatal(err)
	}
	if err := h.SetSessionModel("selected", "alternate-provider-id"); err == nil {
		t.Fatal("provider ID accepted as undeclared resource")
	}
	if err := h.SetSessionModel("selected", "missing"); err == nil {
		t.Fatal("unknown model accepted")
	}
	h2, err := LoadSource(source, raw, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	if ref, err := h2.SessionModel("selected"); err != nil || ref != "alternate" {
		t.Fatalf("restart model %q %v", ref, err)
	}
	runHarnessTurn(t, h2, "selected", "s1", "selected question")
	if backend.lastReq.Model != "alternate-provider-id" {
		t.Fatalf("wrong provider model: %s", backend.lastReq.Model)
	}
	runHarnessTurn(t, h2, "other", "o1", "other question")
	if backend.lastReq.Model != "gpt-5.6" {
		t.Fatalf("model leaked to other session: %s", backend.lastReq.Model)
	}
	after, _ := json.Marshal(h.doc)
	if string(before) != string(after) {
		t.Fatal("compiled document mutated")
	}
	summary, err := h2.Session("selected")
	if err != nil {
		t.Fatal(err)
	}
	fork, err := h2.Fork(context.Background(), ForkRequest{ParentSessionID: summary.ID, SessionID: "child", RunID: "fork", AtSequence: summary.Head})
	if err != nil {
		t.Fatal(err)
	}
	for range fork {
	}
	if ref, err := h2.SessionModel("child"); err != nil || ref != "alternate" {
		t.Fatalf("fork lost boundary model: %q %v", ref, err)
	}
}

func TestSessionPlanDisablesToolsAndRejectsUnexpectedCalls(t *testing.T) {
	h, backend := loadQueueHarness(t)
	stream, err := h.Plan(context.Background(), controlRequest("planning", "p1"))
	if err != nil {
		t.Fatal(err)
	}
	items := controlDrain(t, stream)
	if len(backend.lastReq.Tools) != 0 {
		t.Fatal("plan advertised execution tools")
	}
	emitted := false
	for _, item := range items {
		if item.Type == "output.emitted" {
			emitted = true
		}
		if item.Type == "bashy.run.requested" || item.Type == "tool.call.completed" {
			t.Fatalf("plan executed: %s", item.Type)
		}
	}
	if !emitted {
		t.Fatal("plan emitted no output")
	}
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		return []*api.StreamEvent{{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "bad", Name: "bashy", Input: json.RawMessage(`{"script":"touch forbidden"}`)}}, {Type: "message_delta", Delta: json.RawMessage(`{"stop_reason":"tool_use"}`)}}
	}
	stream, err = h.Plan(context.Background(), controlRequest("planning", "p2"))
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for item := range stream {
		if item.Type == "turn.failed" {
			failed = true
		}
		if item.Type == "tool.call.completed" {
			t.Fatal("unexpected tool executed")
		}
	}
	if !failed {
		t.Fatal("planning accepted unexpected tool call")
	}
}

func TestSessionBtwReplayAndConsumptionAcrossHarnesses(t *testing.T) {
	h, backend := loadQueueHarness(t)
	req := QueueRequest{SessionID: "aside", QueueRef: "interactive", Class: "steering", Text: "remember the aside", IdempotencyKey: "aside-once"}
	if err := h.Enqueue(req); err != nil {
		t.Fatal(err)
	}
	h2, err := Load(h.doc.Source, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Enqueue(req); err != nil {
		t.Fatal(err)
	}
	items, err := h2.TakeQueued("aside", "interactive")
	if err != nil || len(items) != 1 || items[0].Text != req.Text {
		t.Fatalf("replay: %#v %v", items, err)
	}
	items, err = h.TakeQueued("aside", "interactive")
	if err != nil || len(items) != 0 {
		t.Fatalf("consumed aside resurrected: %#v %v", items, err)
	}
}

func TestSessionAsideDoesNotEnterTranscriptAndPlanModePersists(t *testing.T) {
	h, backend := loadQueueHarness(t)
	runHarnessTurn(t, h, "mode", "initial", "original")
	before, err := h.Transcript("mode")
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	if err := h.Enqueue(QueueRequest{SessionID: "mode", QueueRef: "interactive", Class: "steering", Text: "main turn only", IdempotencyKey: "main-only"}); err != nil {
		t.Fatal(err)
	}
	stream, err := h.Btw(context.Background(), controlRequest("mode", "side"))
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	after, _ := h.Transcript("mode")
	if queued, err := h.TakeQueued("mode", "interactive"); err != nil || len(queued) != 1 {
		t.Fatalf("aside consumed main steering: %v %v", queued, err)
	}
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) || len(backend.lastReq.Tools) != 0 {
		t.Fatal("aside changed transcript or advertised tools")
	}
	stream, err = h.Plan(context.Background(), RunRequest{SessionID: "mode"})
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	h2, err := Load(h.doc.Source, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := h2.SessionMode("mode"); err != nil || mode != "plan" {
		t.Fatalf("mode: %q %v", mode, err)
	}
	stream, err = h2.Run(context.Background(), controlRequest("mode", "planned"))
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	if len(backend.lastReq.Tools) != 0 {
		t.Fatal("persisted plan mode allowed tools")
	}
	stream, err = h2.Plan(context.Background(), RunRequest{SessionID: "mode"})
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	if mode, _ := h2.SessionMode("mode"); mode != "act" {
		t.Fatal("toggle did not restore act mode")
	}
}

func TestSessionRetryReplacesCompletedTurn(t *testing.T) {
	h, _ := loadQueueHarness(t)
	runHarnessTurn(t, h, "retry", "first", "one original request")
	request := controlRequest("retry", "second")
	request.Body = nil
	stream, err := h.Retry(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	messages, err := h.Transcript("retry")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, m := range messages {
		if strings.Contains(MessageText(m), "one original request") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("retry duplicated original turn: %d", count)
	}
}

func TestPauseGateWaitsForAllConcurrentStages(t *testing.T) {
	var gate pauseGate
	leave1, err := gate.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	leave2, err := gate.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reached := gate.request()
	leave1()
	select {
	case <-reached:
		t.Fatal("paused while another tool stage remained active")
	default:
	}
	leave2()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("pause missed quiescent boundary")
	}
	gate.release()
}

func TestSessionRevertCompactRetryPreserveEvidence(t *testing.T) {
	h, backend := loadQueueHarness(t)
	runHarnessTurn(t, h, "history", "r1", "first question")
	runHarnessTurn(t, h, "history", "r2", "second question")
	result, err := h.Revert(context.Background(), "history")
	if err != nil {
		t.Fatal(err)
	}
	if !result.TranscriptReverted || result.FilesRestored || !strings.Contains(result.Restoration, "unsupported") {
		t.Fatalf("false restoration claim: %+v", result)
	}
	transcript, err := h.Transcript("history")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range transcript {
		if strings.Contains(MessageText(item), "second question") {
			t.Fatal("reverted input remains")
		}
	}
	summary, _ := h.Session("history")
	fork, err := h.Fork(context.Background(), ForkRequest{ParentSessionID: "history", SessionID: "rewound-child", RunID: "fork-rewound", AtSequence: summary.Head})
	if err != nil {
		t.Fatal(err)
	}
	for range fork {
	}
	compact, err := h.Compact(context.Background(), "history")
	if err != nil || compact.Outcome != "nothing-to-compact" {
		t.Fatalf("compact: %+v %v", compact, err)
	}
	h2, err := Load(h.doc.Source, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	request := controlRequest("history", "retry-second")
	request.Body = nil
	stream, err := h2.Retry(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	controlDrain(t, stream)
	if !strings.Contains(lastUserText(backend.lastReq), "second question") {
		t.Fatalf("retry input lost: %s", lastUserText(backend.lastReq))
	}
	all, err := event.Replay(h.eventPath)
	if err != nil {
		t.Fatal(err)
	}
	sawOriginal := false
	for _, item := range all {
		if item.RunID == "r2" && item.Type == "session.turn-committed" {
			sawOriginal = true
		}
	}
	if !sawOriginal {
		t.Fatal("revert destroyed original evidence")
	}
}

func TestSessionPauseSettlesAndExcludesOtherHarnessMutation(t *testing.T) {
	h, backend := loadQueueHarness(t)
	started, release := make(chan struct{}), make(chan struct{})
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		close(started)
		<-release
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: json.RawMessage(`{"type":"text_delta","text":"finished"}`)}}
	}
	stream, err := h.Run(context.Background(), controlRequest("busy", "active"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	h2, err := Load(h.doc.Source, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.SetSessionModel("busy", "primary"); err == nil {
		t.Fatal("second harness changed active session")
	}
	if _, err := h2.Run(context.Background(), controlRequest("busy", "overlap")); err == nil {
		t.Fatal("second harness admitted overlapping turn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	paused := make(chan error, 1)
	go func() { paused <- h2.Pause(ctx, "busy") }()
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		active := h.active[runKey("busy", "active")]
		active.pause.mu.Lock()
		requested := active.pause.resume != nil
		active.pause.mu.Unlock()
		h.mu.Unlock()
		if requested {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pause was not requested")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	if err := h2.Continue(ctx, "busy"); err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if err := h.SetSessionModel("busy", "primary"); err != nil {
		t.Fatalf("pause did not settle: %v", err)
	}
	if err := h.Pause(ctx, "busy"); err == nil {
		t.Fatal("pause claimed nonexistent live turn")
	}
}

func TestRetryRejectedAdmissionPreservesHistory(t *testing.T) {
	h, _ := loadQueueHarness(t)
	runHarnessTurn(t, h, "admission", "original", "preserve this turn")
	before, _ := h.Transcript("admission")
	want, _ := json.Marshal(before)
	for _, tc := range []struct {
		name   string
		change func(*RunRequest)
	}{
		{"caller", func(r *RunRequest) { r.Principal = "" }},
		{"frontend", func(r *RunRequest) { r.FrontendRef = "unrouted" }},
		{"trigger", func(r *RunRequest) { r.TriggerRef = "missing" }},
		{"body", func(r *RunRequest) { r.Body = []byte("invalid JSON") }},
		{"oversize", func(r *RunRequest) { r.Body = make([]byte, h.doc.Spec.Frontends["embed"].Limits.MaxInputBytes+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := controlRequest("admission", tc.name)
			tc.change(&request)
			if _, err := h.Retry(context.Background(), request); err == nil {
				t.Fatal("invalid retry admitted")
			}
			after, _ := h.Transcript("admission")
			got, _ := json.Marshal(after)
			if string(got) != string(want) {
				t.Fatal("rejected retry changed transcript")
			}
			items, err := event.Replay(h.eventPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.Type == "session.history-replaced" {
					t.Fatal("rejected retry rewound history")
				}
			}
		})
	}
}

func TestSessionControlGraphHonorsContextAndHooks(t *testing.T) {
	for _, aside := range []bool{false, true} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("aside=%t/reject=%t", aside, reject), func(t *testing.T) {
				h, backend := loadQueueHarness(t)
				raw, err := os.ReadFile(h.doc.Source)
				if err != nil {
					t.Fatal(err)
				}
				var doc map[string]any
				if err := yaml.Unmarshal(raw, &doc); err != nil {
					t.Fatal(err)
				}
				s := doc["spec"].(map[string]any)
				pipelines := s["pipelines"].(map[string]any)
				var hook map[string]any
				if err := yaml.Unmarshal([]byte(`
inputs: {request: ycode.input/v1}
outputs: {request: ycode.input/v1}
state: {context: {type: ycode.context/v1, writer: single}}
concurrency: 1
failFast: true
nodes:
  - id: check-context
    needs: []
    run:
      stage: context.load
      with: {contextRef: coding}
      out: {context: context}
`), &hook); err != nil {
					t.Fatal(err)
				}
				pipelines["control-check"] = hook
				s["hooks"].(map[string]any)["control-check"] = map[string]any{"phase": "before-input", "pipelineRef": "control-check", "inputType": "ycode.input/v1", "outputType": "ycode.input/v1", "order": 1, "maxInvocations": 1, "reentrant": false, "failure": "fail"}
				graph := pipelines["session-control"].(map[string]any)
				nodes := graph["nodes"].([]any)
				nodes[0].(map[string]any)["needs"] = []any{"control-check"}
				graph["nodes"] = append([]any{map[string]any{"id": "control-check", "needs": []any{}, "run": map[string]any{"hook.invoke": "control-check", "in": map[string]any{"request": "request"}}}}, nodes...)
				if reject {
					contexts := s["contexts"].(map[string]any)
					rawContext, _ := json.Marshal(contexts["coding"])
					var restricted map[string]any
					if err := json.Unmarshal(rawContext, &restricted); err != nil {
						t.Fatal(err)
					}
					restricted["budget"].(map[string]any)["maxTokens"] = 1
					contexts["hook-only"] = restricted
					hook["nodes"].([]any)[0].(map[string]any)["run"].(map[string]any)["with"].(map[string]any)["contextRef"] = "hook-only"
				}
				raw, _ = yaml.Marshal(doc)
				h, err = LoadSource(h.doc.Source, raw, WithHarnessProvider("openai", backend))
				if err != nil {
					t.Fatal(err)
				}
				request := controlRequest("graph", "control")
				var stream <-chan Event
				if aside {
					stream, err = h.Btw(context.Background(), request)
				} else {
					stream, err = h.Plan(context.Background(), request)
				}
				if err != nil {
					t.Fatal(err)
				}
				failed, hookRan, contextRan := false, false, false
				for item := range stream {
					if item.Type == "turn.failed" {
						failed = true
					}
					if item.Type == "context.loaded" {
						if item.StageID == "check-context" {
							hookRan = true
						} else {
							contextRan = true
						}
					}
				}
				if reject {
					if !failed || len(backend.requests) != 0 {
						t.Fatal("declared hook context budget did not prevent inference")
					}
				} else if failed || !hookRan || !contextRan {
					t.Fatalf("graph bypass: failed=%t hook=%t context=%t", failed, hookRan, contextRan)
				}
			})
		}
	}
}

func TestPauseGateRejectsCompletedRun(t *testing.T) {
	var gate pauseGate
	if err := gate.finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gate.request() != nil {
		t.Fatal("completed run acknowledged a pause")
	}
}

func TestRetryBindsCurrentTriggerAgent(t *testing.T) {
	h, backend := loadQueueHarness(t)
	raw, err := os.ReadFile(h.doc.Source)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	s := doc["spec"].(map[string]any)
	agents := s["agents"].(map[string]any)
	agents["restricted"] = agents["coder"]
	triggers := s["triggers"].(map[string]any)
	original := triggers["interactive-input"].(map[string]any)
	second := map[string]any{}
	for k, v := range original {
		second[k] = v
	}
	route := map[string]any{}
	for k, v := range original["route"].(map[string]any) {
		route[k] = v
	}
	route["agentRef"] = "restricted"
	second["route"] = route
	triggers["restricted-input"] = second
	raw, _ = yaml.Marshal(doc)
	h, err = LoadSource(h.doc.Source, raw, WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	runHarnessTurn(t, h, "authority", "old", "original")
	request := controlRequest("authority", "new")
	request.TriggerRef = "restricted-input"
	stream, err := h.Retry(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items := controlDrain(t, stream)
	for _, item := range items {
		if item.Type != "session.requested" {
			continue
		}
		var data struct {
			Ref string `json:"request_ref"`
		}
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		raw, err := h.Payload(data.Ref)
		if err != nil {
			t.Fatal(err)
		}
		var recorded RunRequest
		if err := json.Unmarshal(raw, &recorded); err != nil {
			t.Fatal(err)
		}
		if recorded.AgentRef != "restricted" {
			t.Fatalf("stale agent authority: %q", recorded.AgentRef)
		}
		return
	}
	t.Fatal("missing replacement request")
}

// The controller is an actual second OS process: it cannot access h.active.
func TestSessionControlProcess(t *testing.T) {
	if action := os.Getenv("YCODE_TEST_CONTROL_ACTION"); action != "" {
		h, err := Load(os.Getenv("YCODE_TEST_CONTROL_SOURCE"))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if action == "pause" {
			err = h.Pause(ctx, "process-session")
		} else {
			err = h.Continue(ctx, "process-session")
		}
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	h, backend := loadQueueHarness(t)
	started, release := make(chan struct{}), make(chan struct{})
	backend.streamFunc = func(*api.Request) []*api.StreamEvent {
		close(started)
		<-release
		return []*api.StreamEvent{{Type: "content_block_delta", Delta: json.RawMessage(`{"type":"text_delta","text":"finished"}`)}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stream, err := h.Run(ctx, controlRequest("process-session", "owned"))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	command := func(action string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionControlProcess$")
		cmd.Env = append(os.Environ(), "YCODE_TEST_CONTROL_ACTION="+action, "YCODE_TEST_CONTROL_SOURCE="+h.doc.Source)
		return cmd
	}
	paused := make(chan error, 1)
	go func() {
		out, err := command("pause").CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, out)
		}
		paused <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		items, err := event.Replay(h.eventPath)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.Type == "session.control-requested" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child sent no durable pause")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-paused:
		t.Fatalf("pause acknowledged during provider stage: %v", err)
	default:
	}
	// Wait for owner consumption before completing the in-flight stage.
	for {
		h.mu.Lock()
		a := h.active[runKey("process-session", "owned")]
		a.pause.mu.Lock()
		requested := a.pause.resume != nil
		a.pause.mu.Unlock()
		h.mu.Unlock()
		if requested {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not consume pause")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	if out, err := command("continue").CombinedOutput(); err != nil {
		t.Fatalf("continue: %v %s", err, out)
	}
	controlDrain(t, stream)
}
