package main

import (
	"fmt"
	"os"
	"strings"
)

// maybeHandleShellCmd intercepts `ycode shell …` before cobra sees it.
// Returns true if it handled the call (caller should return immediately).
//
// Why bypass cobra: the wrapper at ~/bin/ycode-wrappers/bash is a shebang
// (#!/usr/bin/env -S ycode shell --agent) that makes ycode stand in for
// /bin/bash. Foreign agents invoke it with bash flags (-l, -lc, -i,
// --login, ...). Cobra either rejects them as unknown or, in patterns
// like `bash -c -l "<cmd>"`, binds -l as the value of -c — leaving the
// bash interpreter to exec "-l" and report "executable file not found in
// $PATH". The interceptor parses argv with bash semantics so combined
// short flags (-lc) and the value-taking -c flag behave correctly.
//
// Pattern mirrors maybeHandleGiteaHook in cmd/ycode/hook.go.
func maybeHandleShellCmd() bool {
	if len(os.Args) < 2 || os.Args[1] != "shell" {
		return false
	}
	f, helpReq, err := parseShellArgs(os.Args[2:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ycode shell: %v\n", err)
		os.Exit(2)
	}
	if helpReq {
		fmt.Fprintln(os.Stdout, newShellCmd().Long)
		return true
	}
	if err := runShellCmd(nil, nil, f); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

// parseShellArgs is a bash-flavored getopt over ycode shell's flag
// schema. Returns the populated *shellFlags and a help-requested bit.
//
// Recognized flags:
//
//   - ycode-agentic: --agent, --quiet, --no-tui, --json, --manifest,
//     --sandbox, --offline, --suggest, --workdir, --permission,
//     --command/-c, --mine, --mine-history-file, --audit-log,
//     --timeout, --allowed-dirs.
//   - bash compat (parsed, applied as no-ops): -l/--login,
//     -i/--interactive, -r/--restricted, -s/--stdin, -v/--verbose,
//     -x/--xtrace, --posix, --norc, --noprofile, --rcfile <f>,
//     --init-file <f>.
//   - help: -h, --help.
//
// `-c` follows bash semantics, not Go-flag semantics. Bash treats `-c`
// as a mode flag: option processing continues past it, and the FIRST
// non-option positional becomes the command. `bash -c -l "<cmd>"`
// therefore runs `<cmd>` (with `-l` consumed as the login flag), not
// `-l` (which our prior implementation produced — and which surfaced
// as `"-l": executable file not found in $PATH` from the inner exec).
// Inline form `-cVALUE` still works. `bash -c <cmd> arg0 arg1 ...`
// puts trailing positionals in $0/$1/...; ycode shell does not expose
// those, so they are silently dropped.
func parseShellArgs(args []string) (*shellFlags, bool, error) {
	f := &shellFlags{permission: "danger-full-access"}
	helpReq := false
	wantCmd := false // -c was seen; first remaining positional is the command

	long := longFlagTable(f)

	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			// End of option processing. Per bash, when `-c` was given
			// and no command has been seen yet, the next argv entry
			// (verbatim, even if it starts with `-`) is the command.
			if wantCmd && f.command == "" && i+1 < len(args) {
				f.command = args[i+1]
			}
			return f, helpReq, nil
		case a == "--help" || a == "-h":
			helpReq = true
			i++
		case strings.HasPrefix(a, "--"):
			consumed, err := applyLongFlag(a[2:], args, i, long)
			if err != nil {
				return nil, false, err
			}
			i += consumed
		case len(a) > 1 && a[0] == '-':
			consumed, err := applyShortRun(a[1:], f, &helpReq, &wantCmd)
			if err != nil {
				return nil, false, err
			}
			i += consumed
		default:
			// Positional. The first one after a bare `-c` is the
			// command (bash semantics). Subsequent positionals — and
			// any positionals before `-c` (e.g., the wrapper-path
			// injected by the shebang) — are discarded.
			if wantCmd && f.command == "" {
				f.command = a
			}
			i++
		}
	}
	if wantCmd && f.command == "" {
		return nil, false, fmt.Errorf("-c requires a value")
	}
	return f, helpReq, nil
}

// longFlagSpec describes one long flag. Exactly one of bool/str/strs is
// non-nil for flags whose value we keep; ignore=true marks a flag we
// recognize but discard. takesValue=true means the flag consumes a
// following value (after `=` or as the next argv entry).
type longFlagSpec struct {
	bool       *bool
	str        *string
	strs       *[]string
	ignore     bool
	takesValue bool
}

func longFlagTable(f *shellFlags) map[string]longFlagSpec {
	return map[string]longFlagSpec{
		// ycode-agentic boolean flags.
		"agent":    {bool: &f.agent},
		"quiet":    {bool: &f.quiet},
		"no-tui":   {bool: &f.noTUI},
		"json":     {bool: &f.json},
		"manifest": {bool: &f.manifestOnly},
		"sandbox":  {bool: &f.sandbox},
		"offline":  {bool: &f.offline},

		// ycode-agentic value-taking flags.
		"command":           {str: &f.command, takesValue: true},
		"workdir":           {str: &f.workDir, takesValue: true},
		"permission":        {str: &f.permission, takesValue: true},
		"suggest":           {str: &f.suggest, takesValue: true},
		"mine":              {str: &f.mine, takesValue: true},
		"mine-history-file": {str: &f.mineFile, takesValue: true},
		"audit-log":         {str: &f.auditLog, takesValue: true},
		"timeout":           {str: &f.timeoutString, takesValue: true},
		"allowed-dirs":      {strs: &f.allowedDirs, takesValue: true},

		// bash compat — parse-and-discard (no-op).
		"login":       {ignore: true},
		"interactive": {ignore: true},
		"restricted":  {ignore: true},
		"stdin":       {ignore: true},
		"posix":       {ignore: true},
		"verbose":     {ignore: true},
		"xtrace":      {ignore: true},
		"norc":        {ignore: true},
		"noprofile":   {ignore: true},
		"rcfile":      {ignore: true, takesValue: true},
		"init-file":   {ignore: true, takesValue: true},
	}
}

// applyLongFlag handles one --name or --name=value argv entry. Returns
// the number of argv entries consumed (1 for inline value or boolean,
// 2 when the value is the next argv entry).
func applyLongFlag(body string, args []string, idx int, table map[string]longFlagSpec) (int, error) {
	name := body
	var val string
	hasVal := false
	if eq := strings.IndexByte(body, '='); eq >= 0 {
		name = body[:eq]
		val = body[eq+1:]
		hasVal = true
	}

	spec, ok := table[name]
	if !ok {
		return 0, fmt.Errorf("unknown flag --%s", name)
	}

	consumed := 1
	if spec.takesValue && !hasVal {
		if idx+1 >= len(args) {
			return 0, fmt.Errorf("--%s requires a value", name)
		}
		val = args[idx+1]
		consumed = 2
	}
	if !spec.takesValue && hasVal {
		// `--bool=foo` is invalid for boolean flags.
		return 0, fmt.Errorf("--%s does not take a value", name)
	}

	switch {
	case spec.ignore:
		// Parsed and discarded.
	case spec.bool != nil:
		*spec.bool = true
	case spec.str != nil:
		*spec.str = val
	case spec.strs != nil:
		*spec.strs = append(*spec.strs, splitCSV(val)...)
	}
	return consumed, nil
}

// applyShortRun handles one argv entry of combined short flags (the
// part after the leading `-`). Returns the number of argv entries
// consumed (always 1; `-c`'s value, when not inline, is picked up by
// the main loop as the first remaining positional — see
// parseShellArgs's bash-semantics comment).
//
// In a run like `-lc value`, chars before `c` are bool no-ops; `c`
// either takes the rest of the run inline (`-cVALUE`) or sets wantCmd
// so the next non-option positional becomes the command.
func applyShortRun(run string, f *shellFlags, helpReq *bool, wantCmd *bool) (int, error) {
	for j := 0; j < len(run); j++ {
		ch := run[j]
		switch ch {
		case 'h':
			*helpReq = true
		case 'l', 'i', 'r', 's', 'v', 'x':
			// bash compat no-op
		case 'c':
			if j+1 < len(run) {
				// Inline form: -cVALUE.
				f.command = run[j+1:]
				return 1, nil
			}
			// Bare -c: bash continues option processing and takes the
			// first non-option arg as the command. Just record intent.
			*wantCmd = true
			return 1, nil
		default:
			return 0, fmt.Errorf("unknown short flag -%c", ch)
		}
	}
	return 1, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
