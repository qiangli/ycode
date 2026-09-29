package turn

// Sprint: #322; Story: #1168; Story-ID: 6b10fe9133a2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnessbashy "github.com/qiangli/ycode/internal/harness/bashy"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

var testRemedy = map[string]any{
	"rules":               []any{"incomplete-preflight"},
	"kinds":               []any{"command", "effectRefinement"},
	"unlessScriptMatches": `^\s*@effects\(`,
	"text":                "send this:\n@effects(\"read,write,exec\")\nfunction f() {\n{{command}}\n}\nf",
}

func incomplete(kinds ...string) hitl.Preflight {
	report := hitl.Preflight{}
	for _, kind := range kinds {
		report.Unsupported = append(report.Unsupported, hitl.UnsupportedFact{Kind: kind, Value: "x", Reason: "why"})
	}
	return report
}

// A denial the agent knows how to fix comes back with the fixed form, the
// model's script verbatim inside it (a heredoc terminator must stay at
// column 0), and only when every YAML condition holds.
func TestDenyRemedyHandsBackTheReadyForm(t *testing.T) {
	r := &Runtime{}
	script := "python - <<'PY'\nprint(1)\nPY\n"
	deny := func(with map[string]any, script string, rule string, report hitl.Preflight) map[string]any {
		t.Helper()
		out := r.deny(context.Background(), pipeline.Invocation{With: with, Inputs: map[string]any{
			"intent":    hitl.Call{ID: "c1", Name: "bashy", Script: script},
			"decision":  hitl.Decision{PolicyRef: "workspace", RuleID: rule, Decision: "deny"},
			"preflight": report,
		}})
		if out.Err != nil {
			t.Fatalf("deny: %v", out.Err)
		}
		return out.Outputs["result"].(map[string]any)
	}
	with := map[string]any{"reason": true, "remedy": testRemedy}
	res := deny(with, script, "incomplete-preflight", incomplete("command"))
	want := "send this:\n@effects(\"read,write,exec\")\nfunction f() {\npython - <<'PY'\nprint(1)\nPY\n}\nf"
	if res["remedy"] != want {
		t.Fatalf("remedy = %q, want %q", res["remedy"], want)
	}
	obs := observationText(res, nil)
	if !strings.HasPrefix(obs, `denied by policy rule "incomplete-preflight"; the command did not run: why`) || !strings.HasSuffix(obs, "\n"+want) {
		t.Fatalf("observation = %q", obs)
	}
	for name, res := range map[string]map[string]any{
		"no remedy configured":  deny(map[string]any{"reason": true}, script, "incomplete-preflight", incomplete("command")),
		"another rule":          deny(with, script, "outside-the-workspace", hitl.Preflight{Complete: true}),
		"a kind wrapping can't": deny(with, script, "incomplete-preflight", incomplete("command", "syntax")),
		"already wrapped":       deny(with, "@effects(\"read\")\nfunction g() { python x.py; }\ng", "incomplete-preflight", incomplete("command")),
	} {
		if _, has := res["remedy"]; has {
			t.Errorf("%s: unexpected remedy %q", name, res["remedy"])
		}
	}
	bad := r.deny(context.Background(), pipeline.Invocation{With: map[string]any{"remedy": map[string]any{"rules": []any{"incomplete-preflight"}}}, Inputs: map[string]any{
		"intent": hitl.Call{ID: "c1", Name: "bashy", Script: script}, "decision": hitl.Decision{RuleID: "incomplete-preflight"}}})
	if bad.Err == nil {
		t.Fatal("a remedy without text must fail the stage closed")
	}
}

// End to end against genie's own YAML and the real Bashy preflight: the
// commands genie's SWE-bench runs were denied (a bare python heredoc edit,
// awk, a bare test run) get a remedy, and the remedy's form is itself
// preflight-complete and allowed by the workspace policy — no second denial.
func TestGenieRemedyFormPassesPreflightAndPolicy(t *testing.T) {
	scratchStores(t)
	t.Setenv("BASHY_SELF", filepath.Join(t.TempDir(), "no-such-bashy"))
	root, err := os.MkdirTemp(".", ".genie-remedy-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	genie := filepath.Join("..", "..", "..", "examples", "genie")
	raw, err := os.ReadFile(filepath.Join(genie, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join(genie, "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "system.md"), prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	var with map[string]any
	for _, node := range doc.Spec.Pipelines["deny-command"].Nodes {
		if node.Run.Stage == "bashy.deny" {
			with = node.Run.With
		}
	}
	if with == nil || with["remedy"] == nil {
		t.Fatalf("genie deny-command carries no remedy: %v", with)
	}
	events, err := event.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	execution := doc.Spec.Bashy.Execution
	execution.ToolName = "bashy"
	executor, err := harnessbashy.NewExecutor(execution, workspace, harnessbashy.RuntimeOptions{ControlRoot: filepath.Join(root, "control"), AuthorizationKey: []byte("0123456789abcdef0123456789abcdef")}, events)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: executor})
	if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"python heredoc edit": "python - <<'PY'\nfrom pathlib import Path\np = Path('django/db/models/query.py')\np.write_text(p.read_text().replace('a', 'b'))\nPY",
		"awk":                 `awk 'length($0) > 119 { print FILENAME ":" FNR }' django/db/models/query.py tests/lookup/tests.py`,
		"test run":            "python tests/runtests.py lookup --verbosity=1",
	} {
		check := func(call hitl.Call) (hitl.Preflight, hitl.Decision) {
			t.Helper()
			meta := hitl.Meta{SessionID: "s", RunID: "r", StageID: "preflight", ConfigDigest: "sha256:config", IdempotencyKey: name + call.ID, PlacementID: "local"}
			report, err := executor.Preflight(context.Background(), meta, call)
			if err != nil {
				t.Fatalf("%s: preflight: %v", name, err)
			}
			decision, err := controller.Evaluate(meta, "workspace", report)
			if err != nil {
				t.Fatalf("%s: evaluate: %v", name, err)
			}
			return report, decision
		}
		bare := hitl.Call{ID: "bare", Name: "bashy", Script: script}
		report, decision := check(bare)
		if decision.Decision != "deny" || decision.RuleID != "incomplete-preflight" {
			t.Fatalf("%s: bare form = %s/%s, want deny/incomplete-preflight (%v)", name, decision.Decision, decision.RuleID, report.Unsupported)
		}
		remedy, err := denialRemedy(with["remedy"], decision, report, bare)
		if err != nil || remedy == "" {
			t.Fatalf("%s: no remedy (%v) for %v", name, err, report.Unsupported)
		}
		form := remedy[strings.Index(remedy, "@effects"):]
		report, decision = check(hitl.Call{ID: "wrapped", Name: "bashy", Script: form})
		if !report.Complete || decision.Decision != "allow" {
			t.Fatalf("%s: remedy form = %s/%s complete=%v %v\n%s", name, decision.Decision, decision.RuleID, report.Complete, report.Unsupported, form)
		}
		if again, _ := denialRemedy(with["remedy"], hitl.Decision{RuleID: "incomplete-preflight"}, incomplete("command"), hitl.Call{Script: form}); again != "" {
			t.Fatalf("%s: a wrapped form must not be wrapped again", name)
		}
	}
}
