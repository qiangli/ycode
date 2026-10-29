package bashy

import (
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"
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
