package builtins

import (
	"context"
	"fmt"
)

func init() { Register(&sandboxVerb{}) }

type sandboxVerb struct{}

func (sandboxVerb) Name() string { return "sandbox" }
func (sandboxVerb) Description() string {
	return "Sandbox execution is delegated outside lean ycode"
}
func (sandboxVerb) Usage() string { return "yc sandbox -- <command> [args…]" }

func (sandboxVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: missing command (use `--` to separate flags from the command)")
		return 2, nil
	}
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: missing command")
		return 2, nil
	}

	fmt.Fprintln(stdio.Stderr, "yc sandbox: not available in lean ycode; run ycode under bashy or another external sandbox wrapper")
	return 1, nil
}
