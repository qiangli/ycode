package bash

// Blank-import coreutils commands so their init() registers them in the
// tool registry that coreutilsshell.Handler() resolves. Without these,
// ycode's shell has coreutils.Handler() in its exec chain but an EMPTY
// registry, so every command falls through to a real fork of the system
// binary (BSD grep on macOS, GNU on Linux) — inconsistent across hosts and
// with no access to the agentic verbs.
//
// Registering them makes ycode's shell actually bashy: these run IN-PROCESS
// (pure Go, fork-free), consistently across platforms, and — crucially —
// they COMPOSE. The structured/agentic capabilities become composable shell
// verbs the model can pipe (`ast symbols ./pkg | grep --json func | ...`,
// `graph query ...`), instead of non-composable typed tools that force a
// model round-trip per step.
//
// Scoped to the agentic search/structure verbs for now (grep carries the
// new --json structured output; ast/graph are pure additions that shadow no
// system command). Widening to the full coreutils userland (cmds/all) is a
// deliberate follow-up once this slice is measured.
import (
	_ "github.com/qiangli/coreutils/cmds/ast"
	_ "github.com/qiangli/coreutils/cmds/graph"
	_ "github.com/qiangli/coreutils/cmds/grep"
)
