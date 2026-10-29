package bashy

// Sprint: #531; Story: #57; Story-ID: f7ce5d2016a9

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// The D3 live rerun: genie refused `bashy delegate` as incomplete-preflight
// and its deny remedy taught @contain(net: deny), which then failed on WSL
// (no default route) and native Windows (no podman). Under genie's own YAML a
// plain delegate call that meets delegate's guards passes preflight with no
// contain, the same on every OS; anything else is refused as before.
func genieDelegateHarness(t *testing.T, allow string) (*Executor, *hitl.Controller) {
	t.Helper()
	t.Setenv("BASHY_HOME", filepath.Join(t.TempDir(), "bashy-home"))
	t.Setenv("BASHY_DELEGATE_ALLOW", allow)
	doc, err := spec.Load(filepath.Join("..", "..", "..", "examples", "genie", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	config := doc.Spec.Bashy.Execution
	config.ToolName = "bashy"
	executor, err := NewExecutor(config, cwd, RuntimeOptions{
		ControlRoot:      filepath.Join(root, "control"),
		AuthorizationKey: []byte("0123456789abcdef0123456789abcdef"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, err := event.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: executor})
	if err != nil {
		t.Fatal(err)
	}
	return executor, controller
}

func genieDecide(t *testing.T, executor *Executor, controller *hitl.Controller, script string) (hitl.Preflight, hitl.Decision) {
	t.Helper()
	report, err := executor.Preflight(context.Background(), testMeta(), hitl.Call{ID: "call-1", Name: "bashy", Script: script})
	if err != nil {
		t.Fatalf("preflight %q: %v", script, err)
	}
	decision, err := controller.Evaluate(testMeta(), "workspace", report)
	if err != nil {
		t.Fatalf("evaluate %q: %v", script, err)
	}
	return report, decision
}

const governedDelegate = "bashy delegate --json --agent genie:x@door --timeout 2m 'review the change and report findings'"

func TestGenieAllowsAGovernedDelegateWithoutContain(t *testing.T) {
	executor, controller := genieDelegateHarness(t, "genie:x@door,genie:y@door")
	report, decision := genieDecide(t, executor, controller, governedDelegate)
	if !report.Complete || decision.Decision != "allow" || decision.RuleID != "governed-delegate" {
		t.Fatalf("decision = %s/%s complete=%v unsupported=%v effects=%v paths=%v", decision.RuleID, decision.Decision, report.Complete, report.Unsupported, report.Effects, report.Paths)
	}
	if !slices.Equal(report.Effects, []string{"exec", "net", "spend"}) || !slices.Equal(report.Paths, []string{delegateScope}) {
		t.Fatalf("policy indexes = effects %v paths %v", report.Effects, report.Paths)
	}
	if report.Call.Script != governedDelegate {
		t.Fatalf("report call = %q, want the model's call unchanged", report.Call.Script)
	}
	sent := executor.delegateRequest(governedDelegate)
	if strings.Contains(sent, "@contain") || !strings.Contains(sent, "@effects(\"exec,net,spend\")") {
		t.Fatalf("bashy request = %q, want an uncontained effects envelope", sent)
	}
	// The positional target form is governed the same way.
	if _, decision := genieDecide(t, executor, controller, "bashy delegate genie:y@door --timeout=90s --idle-timeout 30s 'do it'"); decision.Decision != "allow" {
		t.Fatalf("positional target decision = %s/%s", decision.RuleID, decision.Decision)
	}
}

func TestGenieStillRefusesUngovernedDelegateAndOtherNetCommands(t *testing.T) {
	executor, controller := genieDelegateHarness(t, "genie:x@door")
	for _, script := range []string{
		"bashy delegate --agent genie:z@door --timeout 2m 'review'",                       // not allowed
		"bashy delegate --agent genie:x@door 'review'",                                    // no --timeout
		"bashy delegate --agent genie:x@door --timeout 2m --allow genie:z@door 'r'",       // replaces the allow policy
		"bashy delegate --agent genie:x@door --timeout 2m --sandbox danger-full-access r", // vendor sandbox override
		"bashy delegate --agent genie:x@door --timeout 2m r > /tmp/out.json",              // unproven redirection
		"bashy delegate --agent \"$TARGET\" --timeout 2m r",                               // dynamic target
		"bashy delegate --agent genie:x@door --timeout 2m r; curl https://example.com",    // not one call
	} {
		report, decision := genieDecide(t, executor, controller, script)
		if decision.Decision == "allow" || report.Complete {
			t.Errorf("%q: decision = %s/%s complete=%v, want refused", script, decision.RuleID, decision.Decision, report.Complete)
		}
	}
	// Another net command: the delegate scope is not reachable by a model's
	// own envelope, even one named like the executor's.
	for _, script := range []string{
		"curl https://example.com",
		"bashy fetch https://example.com",
		"@effects(\"exec,net,spend\")\nfunction ycode_governed_delegate() {\ncurl https://example.com\n}\nycode_governed_delegate",
	} {
		_, decision := genieDecide(t, executor, controller, script)
		if decision.Decision == "allow" {
			t.Errorf("%q: decision = %s/allow, want refused", script, decision.RuleID)
		}
	}
	// The refusal names the guard delegate's call missed.
	report, _ := genieDecide(t, executor, controller, "bashy delegate --agent genie:z@door --timeout 2m 'review'")
	found := false
	for _, fact := range report.Unsupported {
		found = found || (fact.Kind == "delegate" && strings.Contains(fact.Reason, "BASHY_DELEGATE_ALLOW"))
	}
	if !found {
		t.Fatalf("unsupported = %v, want the delegate allow refusal", report.Unsupported)
	}
}

func TestGenieRefusesDelegateWithAnEmptyAllow(t *testing.T) {
	executor, controller := genieDelegateHarness(t, "")
	if report, decision := genieDecide(t, executor, controller, governedDelegate); decision.Decision == "allow" || report.Complete {
		t.Fatalf("decision = %s/%s complete=%v, want refused with an empty allow", decision.RuleID, decision.Decision, report.Complete)
	}
}
