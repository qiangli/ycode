package bashy

// Sprint: #531; Story: #57; Story-ID: f7ce5d2016a9

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/bashy/pkg/harnessrunner"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

func runCall(t *testing.T, executor *Executor, script string) harnessrunner.Result {
	t.Helper()
	value, err := executor.Execute(context.Background(), testMeta(), hitl.Call{ID: "call-1", Name: "bashy", Script: script}, digest("allow"))
	if err != nil {
		t.Fatalf("execute %q: %v", script, err)
	}
	result, ok := value.(harnessrunner.Result)
	if !ok {
		t.Fatalf("execute returned %T", value)
	}
	return result
}

// A governed delegate call runs as bashy's own delegate, uncontained: no
// `bashy contain` wrapper, so no podman and no network namespace. The allowed
// target names no launch template, so delegate fails closed (vendor) before
// any launch — after its guards admitted the genie caller, not denied it.
func TestGovernedDelegateRunsUncontained(t *testing.T) {
	executor, _ := genieDelegateHarness(t, "nosuchtool:m")
	result := runCall(t, executor, "bashy delegate --json --agent nosuchtool:m --timeout 30s 'review'")
	if result.Protocol != "ok" {
		t.Fatalf("delegate protocol = %q/%q", result.Protocol, result.Outcome)
	}
	if result.Intent == nil || !result.Intent.Complete {
		t.Fatalf("intent = %#v", result.Intent)
	}
	// D3's Windows intent had ALLOW but no forced CALLER. Check the
	// executed boundary as well as the isolated environment helper.
	caller := false
	for _, variable := range result.Intent.Environment {
		caller = caller || (variable.Name == delegateCallerEnv && variable.ValueRef == "harness://delegate-caller")
	}
	if !caller {
		t.Fatal("governed delegate intent is missing the forced caller")
	}
	for _, command := range result.Intent.Commands {
		if command.Name != "delegate" {
			t.Fatalf("intent commands = %+v, want delegate alone", result.Intent.Commands)
		}
	}
	stdout := strings.TrimSpace(string(outputBytes(t, result)))
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         *struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout[strings.LastIndex(stdout, "\n")+1:]), &envelope); err != nil {
		t.Fatalf("delegate stdout = %q: %v", stdout, err)
	}
	if envelope.SchemaVersion != "bashy-chat-v1" {
		t.Fatalf("delegate envelope = %+v (stdout %q)", envelope, stdout)
	}
	if envelope.Error != nil && (envelope.Error.Kind == "denied" || strings.Contains(envelope.Error.Message, "contain")) {
		t.Fatalf("delegate error = %+v, want the call admitted and uncontained", envelope.Error)
	}
}
