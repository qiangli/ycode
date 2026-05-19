package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os/exec"
	"time"
)

func init() { Register(&runVerb{}) }

type runVerb struct{}

func (runVerb) Name() string { return "run" }
func (runVerb) Description() string {
	return "Execute a command and emit a structured JSON envelope (stdout, stderr, exit, duration_ms)"
}
func (runVerb) Usage() string {
	return "yc run [--json] -- <command> [args…]"
}

// runEnvelope is the JSON shape emitted to stdout when --json is set
// (the default for this verb — the structured envelope is the point).
type runEnvelope struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Exit       int    `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
	Command    string `json:"command"`
}

func (runVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	fs := flag.NewFlagSet("yc run", flag.ContinueOnError)
	fs.SetOutput(stdio.Stderr)
	plain := fs.Bool("plain", false, "passthrough mode: stream stdout/stderr live, no envelope")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc run: missing command (use `--` to separate)")
		return 2, nil
	}

	cmd := exec.CommandContext(ctx, rest[0], rest[1:]...)
	cmd.Dir = cwd

	if *plain {
		cmd.Stdin = stdio.Stdin
		cmd.Stdout = stdio.Stdout
		cmd.Stderr = stdio.Stderr
		start := time.Now()
		err := cmd.Run()
		_ = start
		return exitFromRunErr(err), nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdin = stdio.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	env := runEnvelope{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Exit:       exitFromRunErr(err),
		DurationMS: dur.Milliseconds(),
		Command:    formatCommand(rest),
	}
	enc := json.NewEncoder(stdio.Stdout)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(env); encErr != nil {
		return 1, encErr
	}
	return env.Exit, nil
}

func exitFromRunErr(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func formatCommand(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var b bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return b.String()
}
