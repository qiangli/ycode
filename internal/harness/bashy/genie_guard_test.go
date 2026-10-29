package bashy

// Sprint: #531; Story: #59; Story-ID: 09ca87a77ca4

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"github.com/qiangli/bashy/pkg/harnessrunner"

	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// genieGuarded is the form genie's deny remedy teaches (examples/genie
// agent.yaml deny-command): an effects envelope capped by @guard, no @contain.
func genieGuarded(command string) string {
	return "@effects(\"read,write,exec\")\n@guard(\"read,write,exec\")\nfunction run_checked() {\n" + command + "\n}\nrun_checked"
}

// The D3 Windows runs: genie refused a plain `sh test_slug.sh` and `git
// commit` as incomplete-preflight, and the remedy taught @contain(net: deny),
// which needs podman. Under genie's own YAML the guarded remedy passes
// preflight and policy with no sandbox on any OS; the guard alone discharges
// the network. An unguarded or net-admitting envelope still asks.
func TestGenieGuardedRemedyPassesWithoutContain(t *testing.T) {
	executor, controller := genieDelegateHarness(t, "")
	for _, command := range []string{"sh test_slug.sh", "git commit -m fix"} {
		if report, decision := genieDecide(t, executor, controller, command); decision.Decision != "deny" || report.Complete {
			t.Fatalf("bare %q = %s/%s complete=%v, want incomplete-preflight", command, decision.RuleID, decision.Decision, report.Complete)
		}
		script := genieGuarded(command)
		report, decision := genieDecide(t, executor, controller, script)
		if !report.Complete || decision.Decision != "allow" || decision.RuleID != "declared-envelope" {
			t.Fatalf("guarded %q = %s/%s complete=%v effects=%v paths=%v unsupported=%v", command, decision.RuleID, decision.Decision, report.Complete, report.Effects, report.Paths, report.Unsupported)
		}
		if !slices.Equal(report.Effects, []string{"exec", "read", "write"}) || !slices.Equal(report.Paths, []string{harnessrunner.ScopeDeclared}) {
			t.Fatalf("guarded %q policy indexes = effects %v paths %v", command, report.Effects, report.Paths)
		}
		if strings.Contains(executor.delegateRequest(script), "@contain") {
			t.Fatalf("guarded %q is sent to bashy with a sandbox", command)
		}
	}
	for _, script := range []string{
		"@effects(\"read,write,exec\")\nfunction run_checked() {\nsh test_slug.sh\n}\nrun_checked",                                                // no guard
		"@effects(\"read,write,exec\")\n@guard(\"read,write,exec,net\")\nfunction run_checked() {\nsh test_slug.sh\n}\nrun_checked",               // guard admits net
		"@effects(\"read,write,exec\")\n@guard(\"read,write,exec\")\nfunction other() {\n:\n}\nfunction run_checked() {\nsh x.sh\n}\nrun_checked", // guard is on another function
	} {
		if _, decision := genieDecide(t, executor, controller, script); decision.Decision == "allow" {
			t.Errorf("%q: decision = %s/allow, want the network rule", script, decision.RuleID)
		}
	}
}

// The guard is enforced inside the interpreter, not by a sandbox: guarded
// work runs, and a net command inside the guard is denied before it runs
// (126) — the same on every OS, with no podman. bashy's in-process `fetch`
// (atlas: net,read) is the net command: front-door verbs (`sh`, `git`,
// `curl`) re-enter BASHY_SELF, which is not bashy in a test binary, so the
// work here is in-process too; preflight above covers `sh` and `git commit`.
func TestGenieGuardRunsWorkAndDeniesNet(t *testing.T) {
	executor, _ := genieDelegateHarness(t, "")
	run := runGuarded(t, executor, "work", genieGuarded("printf slug-ok > out.txt && cat out.txt"))
	if exitCode(run) != 0 || !strings.Contains(string(outputBytes(t, run)), "slug-ok") {
		t.Fatalf("guarded work = %+v stdout %q stderr %q", run.Process, outputBytes(t, run), stderrBytes(t, run))
	}
	denied := runGuarded(t, executor, "net", genieGuarded("fetch http://127.0.0.1:9/; echo rc=$?"))
	if !strings.Contains(string(outputBytes(t, denied)), "rc=126") || !strings.Contains(string(stderrBytes(t, denied)), "denied by the effect cap") {
		t.Fatalf("guarded fetch = %+v stderr %q, want denied by the effect cap", denied.Process, stderrBytes(t, denied))
	}
}

func runGuarded(t *testing.T, executor *Executor, key, script string) harnessrunner.Result {
	t.Helper()
	meta := testMeta()
	meta.IdempotencyKey = "guard-" + key
	value, err := executor.Execute(context.Background(), meta, hitl.Call{ID: key, Name: "bashy", Script: script}, digest("allow"))
	if err != nil {
		t.Fatalf("execute %q: %v", script, err)
	}
	return value.(harnessrunner.Result)
}

func stderrBytes(t *testing.T, result harnessrunner.Result) []byte {
	t.Helper()
	if result.Output == nil {
		return nil
	}
	var value []byte
	for _, chunk := range result.Output.Stderr {
		decoded, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			t.Fatal(err)
		}
		value = append(value, decoded...)
	}
	return value
}

func exitCode(result harnessrunner.Result) int {
	if result.Process == nil || result.Process.ExitCode == nil {
		return -1
	}
	return *result.Process.ExitCode
}
