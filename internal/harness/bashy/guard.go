package bashy

// Sprint: #531; Story: #59; Story-ID: 09ca87a77ca4

import (
	"strings"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"
	"github.com/qiangli/yoke/pkg/atlas"
	"mvdan.cc/sh/v3/syntax"

	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// Guarded envelopes (Sprint 531 D3, superseding the podman design). A Bash#
// function whose body bashy cannot prove command by command is bounded by its
// @effects declaration; bashy's preflight then adds a possible net effect
// (source "uncontained") unless @contain(net: "deny") takes net out — and
// contain needs an OS sandbox (podman on Windows, a default route on WSL).
// @guard is bashy's in-interpreter effect cap: every command the call
// dispatches whose atlas effects exceed it, net included, is denied before it
// runs, on every OS, with no setup. A guard that leaves out net therefore
// discharges the network the same way contain does, and the possible net
// effect is dropped here so policy sees only the declared envelope.
//
// docs/patches/bashy-guard-discharges-net.patch makes bashy's compiler stop
// emitting that fact itself; once the bashy pin carries it this is a no-op.

const uncontainedSource = "uncontained"

// netGuardedFuncs returns the script-defined functions whose @guard cap is a
// static list that excludes net. As in bashy's compiler, a later definition
// of the same name replaces an earlier one.
func netGuardedFuncs(script string) map[string]bool {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBashPP)).Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}
	guarded := map[string]bool{}
	syntax.Walk(file, func(node syntax.Node) bool {
		decl, ok := node.(*syntax.FuncDecl)
		if !ok || decl.Name == nil || decl.Body == nil {
			return true
		}
		capped := false
		for _, d := range decl.Decorators {
			if d == nil || d.Name == nil || d.Name.Value != "guard" || len(d.Args) != 1 {
				continue
			}
			if len(d.ArgNames) > 0 && d.ArgNames[0] != nil && d.ArgNames[0].Value != "effects" {
				continue
			}
			value, ok := literalWord(d.Args[0])
			if !ok {
				continue
			}
			// Nested guards only narrow: one cap without net denies it.
			excludesNet := true
			for _, effect := range strings.Split(value, ",") {
				excludesNet = excludesNet && strings.TrimSpace(effect) != atlas.EffNet
			}
			capped = capped || excludesNet
		}
		guarded[decl.Name.Value] = capped
		return true
	})
	return guarded
}

// governGuardedReport drops the possible net effect bashy adds to a declared
// envelope whose function's guard excludes net. Every other fact, and every
// envelope without such a guard, is left as bashy compiled it.
func governGuardedReport(report *hitl.Preflight) {
	if !report.Complete {
		return
	}
	var guarded map[string]bool
	kept := report.EffectFacts[:0]
	dropped := false
	for _, fact := range report.EffectFacts {
		if fact.Kind == atlas.EffNet && fact.Scope == harnessrunner.ScopeDeclared && fact.Source == uncontainedSource && strings.HasPrefix(fact.Target, "func:") {
			if guarded == nil {
				guarded = netGuardedFuncs(report.Call.Script)
			}
			if guarded[strings.TrimPrefix(fact.Target, "func:")] {
				dropped = true
				continue
			}
		}
		kept = append(kept, fact)
	}
	report.EffectFacts = kept
	if !dropped {
		return
	}
	effects, paths := make(map[string]bool), make(map[string]bool)
	for _, fact := range kept {
		if fact.Kind == atlas.EffPure {
			continue
		}
		effects[fact.Kind] = true
		if fact.Scope != "" {
			paths[fact.Scope] = true
		}
	}
	report.Effects = sortedKeys(effects)
	report.Paths = sortedKeys(paths)
}
