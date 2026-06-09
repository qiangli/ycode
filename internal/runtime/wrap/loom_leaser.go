package wrap

import (
	"context"
	"path/filepath"
	"strings"
)

// inferLoomLabel derives a short branch / author label from the
// foreign-agent argv. Falls back to "agent" when nothing useful is in
// the args — the LoomLeaser implementation is free to sanitize further
// for its own naming rules.
func inferLoomLabel(args []string) string {
	if len(args) == 0 {
		return "agent"
	}
	bin := filepath.Base(args[0])
	bin = strings.TrimSuffix(bin, ".exe")
	bin = strings.TrimSpace(bin)
	if bin == "" || bin == "." {
		return "agent"
	}
	return bin
}

// LoomLeaser is the abstraction Run uses to satisfy `--loom=auto`
// without taking a hard dependency on pkg/loom. Concrete adapters
// live where they make sense:
//
//   - cmd/ycode/wrap.go (the CLI driver) wires an HTTP-MCP-backed
//     leaser that talks to a running `ycode serve`. The agent runs
//     in a clone fetched from the embedded Gitea.
//
//   - internal/foreman wires an in-process leaser that calls
//     pkg/loom.Service directly when the foreman runs in the same
//     process as serve.
//
//   - tests use a fake leaser that returns deterministic values and
//     asserts the env shape Run applies to the child process.
//
// Returning errors from LeaseForWrap is fine; they propagate as a
// non-zero exit code from Run with a clear "wrap: loom lease: ..."
// prefix.
type LoomLeaser interface {
	// LeaseForWrap allocates a sandbox for a wrap-launched agent.
	//
	//   cwd   — the absolute host project path the agent was invoked
	//           against. Used by the backend to resolve a slug.
	//   label — short identifier for branch + author attribution
	//           (derived from the agent name, e.g. "claude-code").
	//
	// Returns:
	//   loomID       — opaque handle; set as YCODE_LOOM_ID.
	//   sandboxPath  — absolute path Run overrides WorkDir with.
	//   branch       — the lease's working branch; set as
	//                  YCODE_LOOM_BRANCH.
	//   env          — additional env vars to merge into the agent's
	//                  environment (e.g. YCODE_LOOM_BASE, _LABEL).
	//   release      — invoked when the agent process exits to
	//                  release the lease. MUST be safe to call
	//                  multiple times.
	LeaseForWrap(ctx context.Context, cwd, label string) (
		loomID string,
		sandboxPath string,
		branch string,
		env map[string]string,
		release func(),
		err error,
	)
}
