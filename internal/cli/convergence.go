package cli

import (
	"encoding/json"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
)

// defaultConvergenceBudget is the number of CONSECUTIVE exploration-only turns
// a headless run may take before the final-answer grace turn. Deliberately far
// below defaultMaxToolIterations: that ceiling is a runaway backstop and was
// demonstrated ineffective against a model that varies its reads every turn —
// the loop detectors key on repeated signatures and stay silent, and by turn
// 100 the run has burned its context window and its wall-clock for nothing.
// Twelve consecutive turns of nothing but reads and searches — each turn can
// batch many calls — is exploration that has stopped converging, not depth.
// Progress (a write, an edit, a test/build run) resets the count, so this
// never shortens a productive task; see conversation.ConvergencePolicy.
const defaultConvergenceBudget = 12

// newConvergencePolicy resolves the convergence policy for a headless run.
// config.ConvergenceBudget: 0 = default, negative = disabled (nil policy —
// every call site is nil-safe).
func (a *App) newConvergencePolicy() *conversation.ConvergencePolicy {
	budget := defaultConvergenceBudget
	if a.config != nil && a.config.ConvergenceBudget != 0 {
		budget = a.config.ConvergenceBudget
	}
	if budget < 0 {
		return nil
	}
	return conversation.NewConvergencePolicy(budget)
}

// turnMadeProgress reports whether any tool call in the turn is capable of
// advancing task state (write / edit / execute / verify) rather than merely
// exploring it (read / search). One progress-capable call makes the whole
// turn a progress turn.
func (a *App) turnMadeProgress(calls []conversation.ToolCall) bool {
	for _, tc := range calls {
		if toolCallMakesProgress(a.toolRegistry, tc) {
			return true
		}
	}
	return false
}

// toolCallMakesProgress classifies a single tool call.
//
// The registry's declared RequiredMode is the principled signal: a tool the
// permission layer files as ReadOnly cannot change task state, so calling it
// is exploration. The one tool that lies about itself is bash — it requires
// full access but mostly runs read-only pipelines — so its actual command is
// classified instead (bash.CommandMakesProgress). Unknown tools count as
// progress: a guess must never terminate a run.
func toolCallMakesProgress(reg *tools.Registry, tc conversation.ToolCall) bool {
	if tc.Name == "bash" {
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(tc.Input, &in); err == nil && strings.TrimSpace(in.Command) != "" {
			return bash.CommandMakesProgress(in.Command)
		}
		return true
	}
	if reg == nil {
		return true
	}
	spec, ok := reg.Get(tc.Name)
	if !ok {
		return true
	}
	return spec.RequiredMode != permission.ReadOnly
}
