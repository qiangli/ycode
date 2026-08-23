package bash

import (
	"strings"

	"github.com/qiangli/ycode/internal/runtime/bash/shellparse"
)

// verificationRunners are build/test entrypoints whose invocation counts as
// measurable progress for convergence tracking even though ClassifyCommand
// files them under ReadOnly (its default for commands it does not recognize).
// Running the gate IS progress: a model iterating edit→test is working, not
// wandering. The set is deliberately small and coarse — over-matching here is
// safe (it can only make the convergence policy more lenient), while
// under-matching would cut a productive run short.
var verificationRunners = map[string]bool{
	"go":     true,
	"make":   true,
	"cargo":  true,
	"npm":    true,
	"npx":    true,
	"yarn":   true,
	"pnpm":   true,
	"pytest": true,
	"tox":    true,
	"mvn":    true,
	"gradle": true,
	"bazel":  true,
	"dotnet": true,
	"ctest":  true,
	"cmake":  true,
	"rake":   true,
	"bashy":  true,
}

// CommandMakesProgress reports whether a bash command is capable of ADVANCING
// task state, as opposed to pure read/search exploration. Used by the headless
// convergence policy to decide whether a turn resets the exploration budget.
//
//   - Any non-read-only intent (write, destructive, network, process,
//     package, system-admin, unknown) counts as progress: it can change the
//     world, so the turn is not mere exploration.
//   - A recognized build/test runner counts as progress even when
//     ClassifyCommand defaults it to ReadOnly.
//   - An output redirect to a real file is a write, whatever the command.
//   - Everything else (grep, cat, ls, find, git log/status/diff, …) is
//     exploration.
func CommandMakesProgress(command string) bool {
	intent, _ := ClassifyCommand(command)
	if intent != ReadOnly {
		return true
	}
	if nodes, err := shellparse.Parse(command); err == nil && len(nodes) > 0 {
		for _, node := range nodes {
			if verificationRunners[node.Name] {
				return true
			}
			for _, r := range node.Redirects {
				if strings.Contains(r.Op, ">") && r.File != "" && r.File != "/dev/null" {
					return true
				}
			}
		}
		return false
	}
	// Parse failed — fall back to the same string splitting ClassifyCommand uses.
	for _, seg := range splitCommandSegments(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		if verificationRunners[baseCommand(fields[0])] {
			return true
		}
	}
	return false
}
