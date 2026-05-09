package builtins

import (
	"context"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/toolexec"
)

func init() { Register(&gitVerb{}) }

type gitVerb struct{}

func (gitVerb) Name() string { return "git" }
func (gitVerb) Description() string {
	return "Git via native go-git (3-tier fallback: native → host → container)"
}
func (gitVerb) Usage() string { return "yc git <subcommand> [args…]" }

func (gitVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc git: missing subcommand")
		return 2, nil
	}

	// Build the executor lazily; container engine is nil so tier 3 is
	// disabled — host fallback covers anything go-git doesn't.
	exec := toolexec.New(nil, nil)
	exec.Register(toolexec.NewGitDef())

	res, err := exec.Run(ctx, "git", cwd, args...)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc git: %v\n", err)
		return 1, nil
	}
	if res.Stdout != "" {
		fmt.Fprint(stdio.Stdout, res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprint(stdio.Stderr, res.Stderr)
	}
	return res.ExitCode, nil
}
