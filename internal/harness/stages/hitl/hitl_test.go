package hitl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestPolicyEvaluationIsOrderedAndFailClosed(t *testing.T) {
	t.Parallel()
	controller, _, _, _ := testController(t)
	tests := []struct {
		name   string
		report Preflight
		want   string
		rule   string
	}{
		{name: "incomplete", report: report("d0", false, []string{"read"}, []string{"workspace"}), want: "deny", rule: "incomplete-preflight"},
		{name: "destructive first", report: report("d1", true, []string{"read", "destroy"}, []string{"workspace"}), want: "ask", rule: "destructive"},
		{name: "workspace", report: report("d2", true, []string{"read", "write"}, []string{"workspace"}), want: "allow", rule: "workspace"},
		{name: "default deny", report: report("d3", true, []string{"read"}, []string{"outside"}), want: "deny", rule: "default"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			decision, err := controller.Evaluate(testMeta(), "workspace", test.report)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Decision != test.want || decision.RuleID != test.rule {
				t.Fatalf("decision = %#v", decision)
			}
			if test.want == "allow" && decision.Binding == "" {
				t.Fatal("allow decision has no execution binding")
			}
		})
	}
}

func TestAskCheckpointsBeforeWaitingAndApproveSurvivesRestart(t *testing.T) {
	t.Parallel()
	controller, logPath, checkpointPath, factory := testController(t)
	review, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: report("ask-digest", true, []string{"destroy"}, []string{"workspace"}), State: map[string]any{"command": "snapshot"}, HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	if review.Outcome != "waiting" || review.DecisionID == "" || review.ReviewDigest == "" {
		t.Fatalf("review = %#v", review)
	}
	var checkpoint event.Checkpoint
	data, err := os.ReadFile(checkpointPath)
	if err != nil || json.Unmarshal(data, &checkpoint) != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	events, err := event.Replay(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "hitl.review.requested" || events[1].Type != "hitl.waiting" || checkpoint.Sequence != events[0].Sequence {
		t.Fatalf("checkpoint/event ordering = checkpoint %d, events %#v", checkpoint.Sequence, events)
	}

	restarted := factory(t)
	resolution, err := restarted.Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: review.DecisionID, ExpectedVersion: review.Version, ReviewDigest: review.ReviewDigest, ReportDigest: review.ReportDigest, Action: "approve", Actor: "operator"})
	if err != nil || resolution.Action != "approve" || resolution.Binding == "" {
		t.Fatalf("resolution = %#v, %v", resolution, err)
	}
	if _, err := restarted.Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: review.DecisionID, ExpectedVersion: review.Version, ReviewDigest: review.ReviewDigest, ReportDigest: review.ReportDigest, Action: "approve", Actor: "operator"}); err == nil {
		t.Fatal("duplicate decision was accepted")
	}
	if _, err := factory(t).Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: review.DecisionID, ExpectedVersion: review.Version, ReviewDigest: review.ReviewDigest, ReportDigest: review.ReportDigest, Action: "approve", Actor: "operator"}); err == nil {
		t.Fatal("duplicate decision was accepted after restart")
	}
}

func TestStaleCASRejectedWithoutConsumingDecision(t *testing.T) {
	t.Parallel()
	controller, _, _, _ := testController(t)
	review, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: report("cas", true, []string{"destroy"}, []string{"workspace"}), State: "state", HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	stale := ResumeRequest{DecisionID: review.DecisionID, ExpectedVersion: review.Version + 1, ReviewDigest: review.ReviewDigest, ReportDigest: review.ReportDigest, Action: "reject", Actor: "operator"}
	if _, err := controller.Resume(context.Background(), testMeta(), stale); err == nil {
		t.Fatal("stale CAS was accepted")
	}
	stale.ExpectedVersion = review.Version
	stale.ReportDigest = "changed"
	if _, err := controller.Resume(context.Background(), testMeta(), stale); err == nil {
		t.Fatal("stale report digest was accepted")
	}
	stale.ReportDigest = review.ReportDigest
	if _, err := controller.Resume(context.Background(), testMeta(), stale); err != nil {
		t.Fatalf("valid rejection failed after stale attempts: %v", err)
	}
}

func TestEditIsOneUseAndRepreflighted(t *testing.T) {
	t.Parallel()
	controller, _, _, _ := testController(t)
	review, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: report("old", true, []string{"destroy"}, []string{"workspace"}), State: "state", HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	edited := Call{ID: "call", Name: "bashy", Script: "curl example", TimeoutMS: 10}
	resolution, err := controller.Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: review.DecisionID, ExpectedVersion: review.Version, ReviewDigest: review.ReviewDigest, ReportDigest: review.ReportDigest, Action: "edit", Actor: "operator", EditedCall: &edited})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Outcome != "repreflighted" || resolution.EditedReport == nil || !reflect.DeepEqual(resolution.EditedReport.Call, edited) || resolution.EditedPolicy == nil || resolution.EditedPolicy.Decision != "ask" {
		t.Fatalf("edit resolution = %#v", resolution)
	}
	if resolution.EditCount != 1 {
		t.Fatalf("edit count = %d", resolution.EditCount)
	}
	second, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: *resolution.EditedReport, State: "edited-state", HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err = controller.Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: second.DecisionID, ExpectedVersion: second.Version, ReviewDigest: second.ReviewDigest, ReportDigest: second.ReportDigest, Action: "edit", Actor: "operator", EditedCall: &edited})
	if err != nil || resolution.EditCount != 2 {
		t.Fatalf("second edit = %#v, %v", resolution, err)
	}
	third, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: *resolution.EditedReport, State: "edited-again", HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(context.Background(), testMeta(), ResumeRequest{DecisionID: third.DecisionID, ExpectedVersion: third.Version, ReviewDigest: third.ReviewDigest, ReportDigest: third.ReportDigest, Action: "edit", Actor: "operator", EditedCall: &edited}); err == nil {
		t.Fatal("third edit exceeded durable maxEdits")
	}
}

func TestUnavailableHumanAndConcurrentResumeFailClosed(t *testing.T) {
	t.Parallel()
	controller, _, checkpointPath, _ := testController(t)
	review, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: report("unavailable", true, []string{"destroy"}, []string{"workspace"}), HumanAvailable: false})
	if err != nil || review.Outcome != "deny" {
		t.Fatalf("unavailable review = %#v, %v", review, err)
	}
	if _, err := os.Stat(checkpointPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unavailable denial unexpectedly checkpointed: %v", err)
	}

	controller, _, _, _ = testController(t)
	waiting, err := controller.Ask(testMeta(), ReviewRequest{PolicyRef: "workspace", Report: report("race", true, []string{"destroy"}, []string{"workspace"}), State: "s", HumanAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	request := ResumeRequest{DecisionID: waiting.DecisionID, ExpectedVersion: waiting.Version, ReviewDigest: waiting.ReviewDigest, ReportDigest: waiting.ReportDigest, Action: "approve", Actor: "operator"}
	var successes atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := controller.Resume(context.Background(), testMeta(), request); err == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful concurrent resumes = %d, want 1", successes.Load())
	}
}

func testController(t *testing.T) (*Controller, string, string, func(*testing.T) *Controller) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	checkpointPath := filepath.Join(dir, "hitl.checkpoint.json")
	events, err := event.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	document := testDocument()
	preflight := fakePreflighter{}
	makeController := func(t *testing.T) *Controller {
		t.Helper()
		controller, err := New(Config{Document: document, Events: events, Payloads: payloads, CheckpointPath: checkpointPath, Preflighter: preflight})
		if err != nil {
			t.Fatal(err)
		}
		return controller
	}
	return makeController(t), logPath, checkpointPath, makeController
}

func testDocument() *spec.Document {
	incomplete := false
	return &spec.Document{Spec: spec.Spec{Policies: map[string]spec.Policy{"workspace": {
		UnavailableHuman: "deny", IncompletePreflight: "deny", Resume: []string{"approve", "edit", "reject"}, ApprovalBinding: "execution-context-digest-v1", MaxEdits: 2,
		Rules: []spec.PolicyRule{
			{ID: "unsupported", Match: spec.PolicyMatch{PreflightComplete: &incomplete}, Decision: "deny"},
			{ID: "destructive", Match: spec.PolicyMatch{EffectsAny: []string{"destroy", "net"}}, Decision: "ask"},
			{ID: "workspace", Match: spec.PolicyMatch{EffectsAllWithin: []string{"read", "write", "exec"}, PathsAllWithin: []string{"workspace"}}, Decision: "allow"},
			{ID: "default", Match: spec.PolicyMatch{Always: true}, Decision: "deny"},
		},
	}}}}
}

func report(digest string, complete bool, effects, paths []string) Preflight {
	return Preflight{Call: Call{ID: "call", Name: "bashy", Script: "echo ok", TimeoutMS: 10}, Digest: digest, Complete: complete, Effects: effects, Paths: paths}
}

func testMeta() Meta {
	return Meta{SessionID: "session", RunID: "run", StageID: "hitl", ConfigDigest: "sha256:config"}
}

type fakePreflighter struct{}

func (fakePreflighter) Preflight(_ context.Context, _ Meta, call Call) (Preflight, error) {
	effects := []string{"read"}
	if strings.Contains(call.Script, "curl") {
		effects = []string{"net"}
	}
	return Preflight{Call: call, Digest: stableID(call.ID, call.Script), Complete: true, Effects: effects, Paths: []string{"workspace"}}, nil
}
