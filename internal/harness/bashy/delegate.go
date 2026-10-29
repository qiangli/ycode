package bashy

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"
	"github.com/qiangli/yoke/pkg/atlas"
	"mvdan.cc/sh/v3/syntax"

	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// delegateGraceMS is the margin a delegate call gets past its own --timeout so
// delegate, which enforces that timeout (and --idle-timeout) itself, can stop
// its child and print the result envelope before the harness ceiling fires.
const delegateGraceMS = 30_000

// wallTimeMS is the ceiling bound into a call's request. Every command gets
// the compiled execution.timeoutMs, except a script that is exactly one
// `bashy delegate` invocation — the declared delegation mechanism, which
// bounds itself: it gets min(its --timeout + grace, jobLifetimeMs), or the job
// lifetime when it gives no --timeout. Without a declared job lifetime nothing
// is widened. A model's timeout_ms still only lowers the ceiling.
func (e *Executor) wallTimeMS(call Call) int {
	timeoutMS := e.config.TimeoutMS
	if lifetime := e.config.JobLifetimeMS; lifetime > 0 {
		if delegateMS, ok := delegateTimeoutMS(call.Script); ok {
			ceiling := lifetime
			if delegateMS > 0 && delegateMS < ceiling-delegateGraceMS {
				ceiling = delegateMS + delegateGraceMS
			}
			timeoutMS = ceiling
		}
	}
	if call.TimeoutMS > 0 && call.TimeoutMS < timeoutMS {
		timeoutMS = call.TimeoutMS
	}
	return timeoutMS
}

// delegateValueFlags are the delegate flags that consume the next word, so a
// value such as `--instruction --timeout` is not mistaken for the flag.
var delegateValueFlags = map[string]bool{
	"--agent": true, "--model": true, "--role": true, "--task": true,
	"--instruction": true, "--file": true, "--context": true, "--cwd": true,
	"--idle-timeout": true, "--allow": true, "--budget-usd": true,
	"--max-tokens": true, "--sandbox": true, "--writable-root": true,
}

// delegateTimeoutMS reports whether script is one plain `bashy delegate`
// command — bare, or as the sole body of an @effects/@contain/@guard envelope
// function that the script then calls once, the form genie's deny remedy
// teaches — and its --timeout in milliseconds (0 when absent). Anything else —
// a pipeline, a list, a background job, an assignment, a command or process
// substitution, a non-literal or unparseable --timeout — is not a delegate
// call and keeps the ordinary ceiling.
func delegateTimeoutMS(script string) (int, bool) {
	// The runner's dialect, so the envelope parses exactly as it will run.
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBashPP)).Parse(strings.NewReader(script), "")
	if err != nil {
		return 0, false
	}
	nested := false
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			nested = true
		}
		return !nested
	})
	if nested {
		return 0, false
	}
	var call *syntax.CallExpr
	switch len(file.Stmts) {
	case 1:
		call = delegateCall(file.Stmts[0])
	case 2:
		call = envelopedDelegate(file.Stmts[0], file.Stmts[1])
	}
	if call == nil {
		return 0, false
	}
	timeoutMS := 0
	args := call.Args[2:]
	for i := 0; i < len(args); i++ {
		word := args[i].Lit()
		if word == "--" {
			break
		}
		value, found := "", false
		switch {
		case word == "--timeout":
			if i+1 >= len(args) {
				return 0, false
			}
			i++
			value, found = args[i].Lit(), true
		case strings.HasPrefix(word, "--timeout="):
			value, found = strings.TrimPrefix(word, "--timeout="), true
		case delegateValueFlags[word]:
			i++
		}
		if !found {
			continue
		}
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return 0, false
		}
		timeoutMS = int(d.Milliseconds())
	}
	return timeoutMS, true
}

// envelopeDecorators are the decorators that only bound a function's effects;
// one that repeats or reschedules the body (@retry) is not an envelope.
var envelopeDecorators = map[string]bool{"effects": true, "contain": true, "guard": true}

func envelopedDelegate(declStmt, callStmt *syntax.Stmt) *syntax.CallExpr {
	if !plainStmt(declStmt) || len(declStmt.Redirs) != 0 || !plainStmt(callStmt) {
		return nil
	}
	decl, ok := declStmt.Cmd.(*syntax.FuncDecl)
	if !ok || decl.Name == nil {
		return nil
	}
	for _, decorator := range decl.Decorators {
		if decorator.Name == nil || !envelopeDecorators[decorator.Name.Value] {
			return nil
		}
	}
	body, ok := decl.Body.Cmd.(*syntax.Block)
	if !ok || len(decl.Body.Redirs) != 0 || len(body.Stmts) != 1 {
		return nil
	}
	invoke, ok := callStmt.Cmd.(*syntax.CallExpr)
	if !ok || len(invoke.Assigns) != 0 || len(invoke.Args) != 1 || invoke.Args[0].Lit() != decl.Name.Value {
		return nil
	}
	return delegateCall(body.Stmts[0])
}

// delegateCall returns stmt's call when it is a plain foreground
// `bashy delegate ...` with no assignments; redirections are allowed.
func delegateCall(stmt *syntax.Stmt) *syntax.CallExpr {
	if !plainStmt(stmt) {
		return nil
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 || len(call.Args) < 2 || call.Args[0].Lit() != "bashy" || call.Args[1].Lit() != "delegate" {
		return nil
	}
	return call
}

func plainStmt(stmt *syntax.Stmt) bool {
	return !stmt.Background && !stmt.Coprocess && !stmt.Negated
}

// Delegate governance (Sprint 531 D3). bashy's preflight has no refiner for
// `bashy delegate`, so a plain call is incomplete and policy denies it — and
// the network sandbox the deny remedy then taught needs podman (native
// Windows) or a default route (WSL). Delegation is instead governed the same
// way on every OS by delegate's own guards: a genie caller, a nonempty
// BASHY_DELEGATE_ALLOW naming the literal target, a literal --timeout, the
// adapter's credential scrub and caps. A call that meets them is sent to
// bashy inside an @effects envelope declaring exactly the Command Atlas
// maximum for delegate, with no @contain, and its declared effects are
// scoped delegateScope so policy can allow them without allowing any other
// net command. The model's call, and so the policy report's call, is
// unchanged; preflight and execution both bind the same enveloped script.

const (
	// delegateScope labels the effects of a governed delegate call. bashy
	// never produces it: only this executor assigns it, after the guards hold.
	delegateScope    = "delegate"
	delegateEnvelope = "ycode_governed_delegate"
	delegateAllowEnv = "BASHY_DELEGATE_ALLOW"
)

// delegateGovernedFlags are the delegate flags a governed call may carry,
// mapped to whether the flag consumes a value. Anything else — --allow
// (which replaces the allow policy), --sandbox, --writable-root, --cwd,
// --file, --account, --self, --role, --model — leaves the call ungoverned.
var delegateGovernedFlags = map[string]bool{
	"--agent": true, "--timeout": true, "--idle-timeout": true, "--task": true,
	"--instruction": true, "--context": true, "--budget-usd": true, "--max-tokens": true,
	"--json": false,
}

// delegateRequest returns the script bashy runs for call: the governed
// envelope for a governed delegate call, else the call's own script.
func (e *Executor) delegateRequest(script string) string {
	effects, isDelegate, refusal := e.delegateGovernance(script)
	if !isDelegate || refusal != "" {
		return script
	}
	return "@effects(\"" + strings.Join(effects, ",") + "\")\nfunction " + delegateEnvelope + "() {\n" +
		strings.TrimRight(script, "\n") + "\n}\n" + delegateEnvelope + "\n"
}

// governDelegateReport scopes a governed delegate call's declared envelope
// effects delegateScope, and gives an ungoverned one the guard it misses as
// the reason policy reaches the model. Anything else is left as bashy
// compiled it.
func (e *Executor) governDelegateReport(report *hitl.Preflight) {
	_, isDelegate, refusal := e.delegateGovernance(report.Call.Script)
	if !isDelegate {
		return
	}
	if refusal != "" {
		report.Unsupported = append(report.Unsupported, hitl.UnsupportedFact{Kind: "delegate", Value: "bashy delegate", Reason: refusal})
		return
	}
	if !report.Complete {
		return
	}
	paths := make(map[string]bool, len(report.Paths))
	for i, fact := range report.EffectFacts {
		if fact.Scope == harnessrunner.ScopeDeclared && fact.Target == "func:"+delegateEnvelope {
			report.EffectFacts[i].Scope = delegateScope
		}
		if report.EffectFacts[i].Kind != atlas.EffPure && report.EffectFacts[i].Scope != "" {
			paths[report.EffectFacts[i].Scope] = true
		}
	}
	report.Paths = sortedKeys(paths)
}

// delegateGovernance reports whether script is one plain `bashy delegate`
// command and, when it is, either its declared effects (refusal empty) or
// why delegate's guards do not govern it.
func (e *Executor) delegateGovernance(script string) (effects []string, isDelegate bool, refusal string) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBashPP)).Parse(strings.NewReader(script), "")
	if err != nil || len(file.Stmts) != 1 {
		return nil, false, ""
	}
	call := delegateCall(file.Stmts[0])
	if call == nil {
		return nil, false, ""
	}
	if len(file.Stmts[0].Redirs) != 0 {
		return nil, true, "a governed bashy delegate call has no redirections"
	}
	words := make([]string, 0, len(call.Args)-2)
	for _, arg := range call.Args[2:] {
		word, ok := literalWord(arg)
		if !ok {
			return nil, true, "every bashy delegate argument must be a literal"
		}
		words = append(words, word)
	}
	target, timeout, idle := "", "", ""
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--" {
			if target == "" && i+1 < len(words) {
				target = words[i+1]
			}
			break
		}
		if !strings.HasPrefix(word, "-") {
			if target == "" {
				target = word
			}
			continue
		}
		name, value, inline := strings.Cut(word, "=")
		takesValue, known := delegateGovernedFlags[name]
		if !known {
			return nil, true, "bashy delegate " + name + " is not governed by delegate's guards"
		}
		if takesValue && !inline {
			if i+1 >= len(words) {
				return nil, true, "bashy delegate " + name + " needs a value"
			}
			i++
			value = words[i]
		} else if !takesValue && inline {
			return nil, true, "bashy delegate " + name + " takes no value"
		}
		switch name {
		case "--agent":
			target = value
		case "--timeout":
			timeout = value
		case "--idle-timeout":
			idle = value
		}
	}
	if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
		return nil, true, "bashy delegate needs a literal positive --timeout"
	}
	if idle != "" {
		if d, err := time.ParseDuration(idle); err != nil || d < 0 {
			return nil, true, "bashy delegate --idle-timeout must be a literal duration"
		}
	}
	if !e.delegateAllowed(target) {
		return nil, true, "bashy delegate target " + strconv.Quote(target) + " is not in " + delegateAllowEnv
	}
	entry, ok := atlas.Lookup("delegate")
	if !ok {
		return nil, true, "bashy delegate has no Command Atlas classification"
	}
	for _, effect := range entry.Effects {
		if effect == atlas.EffPure {
			continue
		}
		if !slices.Contains(e.config.EffectsCeiling, effect) {
			return nil, true, "bashy delegate effect " + effect + " is outside execution.effectsCeiling"
		}
		effects = append(effects, effect)
	}
	if len(effects) == 0 {
		return nil, true, "bashy delegate has no Command Atlas effects"
	}
	sort.Strings(effects)
	return effects, true, ""
}

// delegateAllowed reports whether target is an entry of the nonempty
// BASHY_DELEGATE_ALLOW the executed command receives, with the genie caller
// identity forced, so delegate itself enforces the same policy. delegate
// also resolves nicknames; an exact entry is the stricter, provable match.
func (e *Executor) delegateAllowed(target string) bool {
	if target == "" || strings.EqualFold(target, "self") {
		return false
	}
	caller, allow := false, ""
	for _, variable := range e.environment() {
		switch variable.Name {
		case delegateCallerEnv:
			caller = variable.Value == delegateCaller
		case delegateAllowEnv:
			allow = variable.Value
		}
	}
	if !caller {
		return false
	}
	for _, entry := range strings.Split(allow, ",") {
		if entry = strings.TrimSpace(entry); entry != "" && entry == target {
			return true
		}
	}
	return false
}

// literalWord returns word's value when it has no expansion: plain,
// single-quoted or double-quoted literal text.
func literalWord(word *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range word.Parts {
		switch value := part.(type) {
		case *syntax.Lit:
			b.WriteString(value.Value)
		case *syntax.SglQuoted:
			if value.Dollar {
				return "", false
			}
			b.WriteString(value.Value)
		case *syntax.DblQuoted:
			if value.Dollar {
				return "", false
			}
			for _, quoted := range value.Parts {
				literal, ok := quoted.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(literal.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}
