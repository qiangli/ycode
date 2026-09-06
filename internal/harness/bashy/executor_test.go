package bashy

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

func testConfig() spec.Bashy {
	return spec.Bashy{
		ToolName: "bashy", TimeoutMS: 2000, JobLifetimeMS: 5000,
		MaxOutputChars: 1024, MaxSpillBytes: 4096,
		Environment: spec.Environment{Inherit: "allowlist", Names: []string{"HARNESS_VALUE"}},
	}
}

func testMeta() Meta {
	return Meta{
		SessionID: "session", RunID: "run", StageID: "bashy-node",
		ConfigDigest: "sha256:config", StateRevision: 7,
		LifecycleGeneration: 3, Attempt: 2,
		IdempotencyKey: "turn-8-call-1", PlacementID: "local",
		PlacementGeneration: 4, PolicyRevision: 9,
	}
}

func testExecutor(t *testing.T, store *event.Store, stdinBytes int64) (*Executor, string) {
	t.Helper()
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(testConfig(), cwd, RuntimeOptions{
		ControlRoot:      filepath.Join(root, "control"),
		AuthorizationKey: []byte("0123456789abcdef0123456789abcdef"),
		StdinBytes:       stdinBytes,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	return executor, cwd
}

func TestPreflightProjectsTypedEvidenceWithoutExecution(t *testing.T) {
	executor, cwd := testExecutor(t, nil, 0)
	call := hitl.Call{ID: "call-1", Name: "bashy", Script: "printf evidence > output.bin"}
	report, err := executor.Preflight(context.Background(), testMeta(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Digest == "" || report.AtlasDigest == "" || len(report.EffectFacts) == 0 {
		t.Fatalf("preflight = %#v", report)
	}
	if !contains(report.Effects, "write") || !contains(report.Effects, "destroy") || !contains(report.Paths, "workspace") {
		t.Fatalf("policy indexes = effects %v paths %v", report.Effects, report.Paths)
	}
	foundTarget := false
	canonicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range report.EffectFacts {
		foundTarget = foundTarget || fact.Target == filepath.Join(canonicalCWD, "output.bin")
	}
	if !foundTarget {
		t.Fatalf("typed target missing: %#v", report.EffectFacts)
	}
	if _, err := os.Stat(filepath.Join(cwd, "output.bin")); !os.IsNotExist(err) {
		t.Fatalf("preflight mutated workspace: %v", err)
	}
}

func TestExecuteBindsAllGraphAuthorityAndBinaryOutput(t *testing.T) {
	root := t.TempDir()
	store, err := event.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	executor, _ := testExecutor(t, store, 0)
	meta := testMeta()
	call := hitl.Call{ID: "binary", Name: "bashy", Script: `printf '\001\002abcdef'`}
	value, err := executor.Execute(context.Background(), meta, call, digest("allow"))
	if err != nil {
		t.Fatal(err)
	}
	result := value.(harnessrunner.Result)
	if result.Protocol != "ok" || result.Outcome != harnessrunner.OutcomeCompleted || result.Binding.RunID != meta.RunID || result.Binding.NodeID != meta.StageID || result.Binding.StateRevision != meta.StateRevision || result.Binding.LifecycleGeneration != meta.LifecycleGeneration || result.Binding.IdempotencyKey != meta.IdempotencyKey {
		t.Fatalf("execute = %#v", result)
	}
	if got := outputBytes(t, result); string(got) != "\x01\x02abcdef" {
		t.Fatalf("stdout = %v", got)
	}
	events, err := event.Replay(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "bashy.requested" || events[1].Type != "bashy.completed" || events[0].ConfigDigest != meta.ConfigDigest || events[0].StateBefore != meta.StateRevision {
		t.Fatalf("events = %#v", events)
	}
}

func TestIncompletePreflightFailsClosedAndRemainsReviewable(t *testing.T) {
	executor, _ := testExecutor(t, nil, 0)
	call := hitl.Call{ID: "dynamic", Name: "bashy", Script: `"$COMMAND" value`}
	report, err := executor.Preflight(context.Background(), testMeta(), call)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete || report.Digest == "" || len(report.Unsupported) == 0 {
		t.Fatalf("incomplete preflight = %#v", report)
	}
	if _, err := executor.Execute(context.Background(), testMeta(), call, digest("allow")); err == nil || !strings.Contains(err.Error(), "incompletePreflight") {
		t.Fatalf("execute error = %v", err)
	}
}

func TestDurableStartStdinAndCursorPoll(t *testing.T) {
	executor, _ := testExecutor(t, nil, 32)
	meta := testMeta()
	started, err := executor.Start(context.Background(), meta, hitl.Call{ID: "durable", Name: "bashy", Script: "read value; printf '%s' done"}, digest("ask-approved"))
	if err != nil {
		t.Fatal(err)
	}
	if started.Outcome != harnessrunner.OutcomeRunning || started.Process == nil {
		t.Fatalf("start = %#v", started)
	}
	meta.StageID = "routed-control-node"
	meta.StateRevision++
	base := Control{Binding: started.Binding, JobID: started.Process.JobID, JobGeneration: started.Process.Generation}
	stdin := base
	stdin.Operation, stdin.RequestID, stdin.InputSequence, stdin.Input = harnessrunner.OperationStdin, "stdin-1", 1, []byte("hello\n")
	accepted, err := executor.Control(context.Background(), meta, stdin)
	if err != nil || accepted.AcceptedInput == nil || *accepted.AcceptedInput != 1 {
		t.Fatalf("stdin = %#v, %v", accepted, err)
	}
	poll := base
	poll.Operation, poll.RequestID, poll.MaxChunkBytes = harnessrunner.OperationPoll, "poll-1", 2
	var terminal harnessrunner.Result
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		terminal, err = executor.Control(context.Background(), meta, poll)
		if err != nil {
			t.Fatal(err)
		}
		if terminal.Outcome != harnessrunner.OutcomeRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if terminal.Outcome != harnessrunner.OutcomeCompleted || terminal.Output == nil {
		t.Fatalf("terminal poll = %#v", terminal)
	}
	var all []byte
	for {
		all = append(all, outputBytes(t, terminal)...)
		if terminal.Output.StdoutNext >= 4 {
			break
		}
		poll.StdoutCursor = terminal.Output.StdoutNext
		poll.RequestID = "poll-next-" + string(rune('0'+poll.StdoutCursor))
		terminal, err = executor.Control(context.Background(), meta, poll)
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(all) != "done" {
		t.Fatalf("cursor output = %q", all)
	}
}

func TestControlRejectsAuthorityDrift(t *testing.T) {
	executor, _ := testExecutor(t, nil, 0)
	control := Control{Operation: harnessrunner.OperationPoll, RequestID: "poll", JobID: "job_0123456789abcdef0123456789abcdef", JobGeneration: 1,
		Binding: harnessrunner.Binding{RunID: "other", NodeID: testMeta().StageID, Attempt: 2, ConfigDigest: testMeta().ConfigDigest, IdempotencyKey: "key"}}
	if _, err := executor.Control(context.Background(), testMeta(), control); err == nil || !strings.Contains(err.Error(), "graph authority") {
		t.Fatalf("authority error = %v", err)
	}
}

func TestExecutorRequiresExternalTrustedControlRoot(t *testing.T) {
	cwd := t.TempDir()
	options := RuntimeOptions{ControlRoot: filepath.Join(cwd, ".control"), AuthorizationKey: []byte("0123456789abcdef0123456789abcdef")}
	if _, err := NewExecutor(testConfig(), cwd, options, nil); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("control root error = %v", err)
	}
	options.ControlRoot = t.TempDir()
	options.AuthorizationKey = []byte("short")
	if _, err := NewExecutor(testConfig(), cwd, options, nil); err == nil || !strings.Contains(err.Error(), "32-byte") {
		t.Fatalf("key error = %v", err)
	}
}

func outputBytes(t *testing.T, result harnessrunner.Result) []byte {
	t.Helper()
	if result.Output == nil {
		return nil
	}
	var value []byte
	for _, chunk := range result.Output.Stdout {
		decoded, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			t.Fatal(err)
		}
		value = append(value, decoded...)
	}
	return value
}

func digest(seed string) string { return stableID(seed) }

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
