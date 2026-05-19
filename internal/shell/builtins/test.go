package builtins

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/qiangli/ycode/internal/tools"
)

func init() { Register(&testVerb{}) }

type testVerb struct{}

func (testVerb) Name() string { return "test" }
func (testVerb) Description() string {
	return "Run tests with structured output (auto-detects go/pytest/jest/vitest/cargo)"
}
func (testVerb) Usage() string {
	return "yc test [--json] [--framework auto|go|pytest|jest|vitest|cargo] [--pattern <re>] [--path <dir>]"
}

func (testVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	fs := flag.NewFlagSet("yc test", flag.ContinueOnError)
	fs.SetOutput(stdio.Stderr)
	jsonOut := fs.Bool("json", false, "emit TestResult as JSON instead of a summary line")
	framework := fs.String("framework", "auto", "test framework (auto|go|pytest|jest|vitest|cargo)")
	pattern := fs.String("pattern", "", "filter pattern passed to the framework")
	path := fs.String("path", "", "directory to test (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}

	dir := *path
	if dir == "" {
		dir = cwd
	}

	fw := *framework
	if fw == "" || fw == "auto" {
		fw = tools.DetectFramework(dir)
	}
	if fw == "" {
		fmt.Fprintln(stdio.Stderr, "yc test: could not detect framework — pass --framework explicitly")
		return 2, nil
	}

	result := tools.RunTests(ctx, fw, dir, *pattern)

	if *jsonOut {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintf(stdio.Stdout, "%s: %d passed, %d failed, %d skipped (%s)\n",
			result.Framework, result.Passed, result.Failed, result.Skipped, result.Duration)
		for _, f := range result.Failures {
			if f.File != "" {
				fmt.Fprintf(stdio.Stdout, "  FAIL %s\n    %s:%d\n", f.Name, f.File, f.Line)
			} else {
				fmt.Fprintf(stdio.Stdout, "  FAIL %s\n", f.Name)
			}
		}
		if result.Error != "" {
			fmt.Fprintf(stdio.Stderr, "yc test: %s\n", result.Error)
		}
	}

	if !result.Success {
		return 1, nil
	}
	return 0, nil
}
