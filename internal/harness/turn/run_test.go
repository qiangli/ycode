package turn

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	harnessbashy "github.com/qiangli/ycode/internal/harness/bashy"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
)

const exampleProbeScript = `script: "printf 'workspace-ready'"`

// scratchStores keeps every store a test can reach inside t.TempDir; a test
// must never read or write the operator's real Bashy or ycode state.
func scratchStores(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HOME", "BASHY_KB_DIR", "BASHY_HOME", "BASHY_SKILLS_DIR", "YCODE_DATA_DIR"} {
		t.Setenv(name, t.TempDir())
	}
}

type bashyRunHarness struct {
	runtime   *Runtime
	eventPath string
	workspace string
}

// newBashyRunRuntime compiles the canonical fixture with the example probe
// node patched and wires the turn runtime. A nil boundary factory selects the
// real digest-bound Bashy executor.
func newBashyRunRuntime(t *testing.T, patch func([]byte) []byte, boundary func(workspace string) BashyBoundary) bashyRunHarness {
	t.Helper()
	scratchStores(t)
	// Temp roots live beside the test so darwin's /var symlink cannot break the
	// compiled readableRoots containment check.
	root, err := os.MkdirTemp(".", ".bashyrun-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		raw = patch(raw)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "events.jsonl")
	events, err := event.Open(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: &recordingDelivery{}, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	var bashyBoundary BashyBoundary
	if boundary != nil {
		bashyBoundary = boundary(workspace)
	} else {
		execution := doc.Spec.Bashy.Execution
		execution.ToolName = "bashy"
		executor, err := harnessbashy.NewExecutor(execution, workspace, harnessbashy.RuntimeOptions{ControlRoot: filepath.Join(root, "control"), AuthorizationKey: []byte("0123456789abcdef0123456789abcdef")}, events)
		if err != nil {
			t.Fatal(err)
		}
		bashyBoundary = executor
	}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashyBoundary})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashyBoundary, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	return bashyRunHarness{runtime: runtime, eventPath: eventPath, workspace: workspace}
}

func replaceProbeScript(t *testing.T, script string) func([]byte) []byte {
	t.Helper()
	return func(raw []byte) []byte {
		mutated := bytes.Replace(raw, []byte(exampleProbeScript), []byte(script), 1)
		if bytes.Equal(mutated, raw) {
			t.Fatalf("fixture does not carry %q", exampleProbeScript)
		}
		return mutated
	}
}

func bashyRunContext() context.Context {
	return context.WithValue(context.Background(), runKey{}, runContext{sessionID: "session", runID: "run-1", agentRef: "coder", originFrontend: "embed"})
}

func bashyRunEvents(t *testing.T, eventPath string) map[string]int {
	t.Helper()
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, item := range replayed {
		counts[item.Type]++
	}
	return counts
}

// hostShellBashy proves the composed-script contract (env exports, stdin JSON,
// stdout capture) against a real shell. It reports the effects a compliant
// analyzer would, so the canonical workspace policy path stays exercised.
type hostShellBashy struct{ dir string }

func (hostShellBashy) Preflight(_ context.Context, _ hitl.Meta, call hitl.Call) (hitl.Preflight, error) {
	return hitl.Preflight{Call: call, Digest: stableID(call.ID, call.Script), Complete: true, Effects: []string{"read"}, Paths: []string{"workspace"}}, nil
}

func (b hostShellBashy) Execute(ctx context.Context, _ hitl.Meta, call hitl.Call, _ string) (any, error) {
	command := exec.CommandContext(ctx, "bash", "-c", call.Script)
	command.Dir = b.dir
	command.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	exit := 0
	if err := command.Run(); err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			return nil, err
		}
		exit = exitError.ExitCode()
	}
	outcome := "completed"
	if exit != 0 {
		outcome = "failed"
	}
	return map[string]any{
		"outcome": outcome,
		"process": map[string]any{"exitCode": exit},
		"output": map[string]any{
			"stdout": []any{map[string]any{"from": 0, "to": stdout.Len(), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(stdout.Bytes())}},
			"stderr": []any{map[string]any{"from": 0, "to": stderr.Len(), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(stderr.Bytes())}},
		},
	}, nil
}

func TestBashyRunStaticScriptLandsStdoutInTypedPort(t *testing.T) {
	harness := newBashyRunRuntime(t, nil, nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe", Inputs: map[string]any{"request": map[string]any{"text": "probe"}}})
	if out.Class != pipeline.OutcomeSucceeded {
		t.Fatalf("outcome = %#v (err %v)", out, out.Err)
	}
	if got, ok := out.Outputs["probe"].(string); !ok || got != "workspace-ready" {
		t.Fatalf("probe = %#v", out.Outputs["probe"])
	}
	counts := bashyRunEvents(t, harness.eventPath)
	if counts["bashy.run.requested"] != 1 || counts["bashy.run.completed"] != 1 {
		t.Fatalf("events = %v", counts)
	}
}

func TestBashyRunProjectsEnvAndStdinIntoTypedPort(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("host shell double requires bash")
	}
	harness := newBashyRunRuntime(t,
		replaceProbeScript(t, `script: 'printf "%s|" "$YCODE_IN_GREETING"; cat'`),
		func(workspace string) BashyBoundary { return hostShellBashy{dir: workspace} })
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe", Inputs: map[string]any{"greeting": "hello-stage"}})
	if out.Class != pipeline.OutcomeSucceeded {
		t.Fatalf("outcome = %#v (err %v)", out, out.Err)
	}
	got, ok := out.Outputs["probe"].(string)
	if !ok || got != `hello-stage|{"greeting":"hello-stage"}` {
		t.Fatalf("probe = %#v", out.Outputs["probe"])
	}
	counts := bashyRunEvents(t, harness.eventPath)
	if counts["bashy.run.requested"] != 1 || counts["bashy.run.completed"] != 1 {
		t.Fatalf("events = %v", counts)
	}
}

func TestBashyRunDeniesWriteEffectUnderReadCeiling(t *testing.T) {
	harness := newBashyRunRuntime(t, replaceProbeScript(t, `script: 'printf blocked > blocked.txt'`), nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe"})
	if out.Class != pipeline.OutcomeFailed || out.Code != "bashy.run-denied" {
		t.Fatalf("outcome = %#v", out)
	}
	if out.Err == nil || !strings.Contains(out.Err.Error(), "exceeds the node ceiling") {
		t.Fatalf("err = %v", out.Err)
	}
	if _, err := os.Stat(filepath.Join(harness.workspace, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied script reached the workspace: %v", err)
	}
	counts := bashyRunEvents(t, harness.eventPath)
	if counts["bashy.run.requested"] != 1 || counts["bashy.run.completed"] != 1 {
		t.Fatalf("events = %v", counts)
	}
}

func TestBashyRunNonZeroExitFailsClosed(t *testing.T) {
	harness := newBashyRunRuntime(t, replaceProbeScript(t, `script: 'exit 7'`), nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe"})
	if out.Class != pipeline.OutcomeFailed || out.Code != "bashy.run-failed" {
		t.Fatalf("outcome = %#v", out)
	}
	if out.Err == nil || !strings.Contains(out.Err.Error(), "exit 7") {
		t.Fatalf("err = %v", out.Err)
	}
}

func TestBashyRunParsesJSONOutPortType(t *testing.T) {
	harness := newBashyRunRuntime(t, func(raw []byte) []byte {
		raw = replaceProbeScript(t, `script: 'printf "{\"ok\":true,\"count\":2}"'`)(raw)
		mutated := bytes.Replace(raw, []byte("probe: {type: string, writer: single}"), []byte("probe: {type: ycode.probe/v1, writer: single}"), 1)
		if bytes.Equal(mutated, raw) {
			t.Fatal("fixture does not declare the probe slot")
		}
		return mutated
	}, nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe"})
	if out.Class != pipeline.OutcomeSucceeded {
		t.Fatalf("outcome = %#v (err %v)", out, out.Err)
	}
	value, ok := out.Outputs["probe"].(map[string]any)
	if !ok || value["ok"] != true || value["count"] != float64(2) {
		t.Fatalf("probe = %#v", out.Outputs["probe"])
	}
}

func TestBashyRunMalformedJSONStdoutFailsClosed(t *testing.T) {
	harness := newBashyRunRuntime(t, func(raw []byte) []byte {
		raw = replaceProbeScript(t, `script: 'printf "not json"'`)(raw)
		return bytes.Replace(raw, []byte("probe: {type: string, writer: single}"), []byte("probe: {type: ycode.probe/v1, writer: single}"), 1)
	}, nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe"})
	if out.Class != pipeline.OutcomeFailed || out.Code != "bashy.run-failed" {
		t.Fatalf("outcome = %#v", out)
	}
	if out.Err == nil || !strings.Contains(out.Err.Error(), "out port type") {
		t.Fatalf("err = %v", out.Err)
	}
}

// The sibling harnessrunner analyzer proves only static command arguments, so
// a script expanding its typed-input environment is incomplete evidence today
// and the stage fails closed. This pins the current boundary posture; see
// docs/todo-notes/fe824e92dc23.md.
// The typed-input mechanism end to end under the REAL boundary: a script that
// consumes $YCODE_IN_<PORT> through an effect-pure builtin compiles complete
// (bashy's harnessrunner proves that variable arguments to printf add no
// governed effect — sprint 164 B4), runs, and its stdout lands in the typed
// port. Until that refiner landed this exact node was denied as incomplete
// preflight; the old pin asserting the denial was deleted with the refiner.
func TestBashyRunEnvConsumptionSucceedsUnderRealBoundary(t *testing.T) {
	harness := newBashyRunRuntime(t, replaceProbeScript(t, `script: 'printf "%s" "$YCODE_IN_GREETING"'`), nil)
	out := harness.runtime.bashyRun(bashyRunContext(), pipeline.Invocation{StageID: "workspace-probe", Inputs: map[string]any{"greeting": "hello"}})
	if out.Class != pipeline.OutcomeSucceeded {
		t.Fatalf("outcome = %#v", out)
	}
	if got := out.Outputs["probe"]; got != "hello" {
		t.Fatalf("typed port probe = %#v, want the env value the script printed", got)
	}
}

func TestTurnConstructionRejectsBashyRunEffectAboveDocumentCeiling(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".bashyrun-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := bytes.Replace(raw, []byte("              effects: [read]\n"), []byte("              effects: [destroy]\n"), 1)
	if bytes.Equal(mutated, raw) {
		t.Fatal("fixture does not carry the example bashy.run effects")
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), mutated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileBashyRunIndex(doc); err == nil || !strings.Contains(err.Error(), "effectsCeiling") {
		t.Fatalf("err = %v", err)
	}
}
