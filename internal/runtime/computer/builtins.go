package computer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/bash/shellparse"
)

// Builtin is the in-process implementation of a single shell-style
// command. It mirrors bash.Executor.Execute's contract so callers
// can treat builtin and forked dispatch interchangeably.
type Builtin func(ctx context.Context, argv []string, workDir string) (*bash.ExecResult, error)

// builtins is the dispatch table for fork-avoidance. Names match
// the binaries an agent typically invokes from the shell. Anything
// not in this map (or any command that uses pipes / redirections /
// command substitution / env expansion) falls through to the real
// bash.Executor — see tryBuiltin.
var builtins = map[string]Builtin{
	"pwd":      bPwd,
	"echo":     bEcho,
	"true":     bTrue,
	":":        bTrue,
	"false":    bFalse,
	"cat":      bCat,
	"ls":       bLs,
	"mkdir":    bMkdir,
	"head":     bHead,
	"tail":     bTail,
	"which":    bWhich,
	"basename": bBasename,
	"dirname":  bDirname,
}

// tryBuiltin attempts in-process dispatch of params.Command. Returns
// (result, true) if a builtin handled it; (nil, false) otherwise (in
// which case the caller should fall through to the real executor).
//
// Eligibility: the command must parse to exactly one CommandNode
// that is not in a pipeline / subshell, not negated, has no
// redirects, no assignments, and whose Name is a key in builtins.
// If any of those checks fails — including parse errors — we fall
// through.
func tryBuiltin(ctx context.Context, p bash.ExecParams) (*bash.ExecResult, bool) {
	if p.Background || p.Stdin != "" {
		return nil, false
	}
	nodes, err := shellparse.Parse(p.Command)
	if err != nil || len(nodes) != 1 {
		return nil, false
	}
	n := nodes[0]
	if n.InPipeline || n.InSubshell || n.Negated {
		return nil, false
	}
	if len(n.Redirects) != 0 || len(n.Assigns) != 0 {
		return nil, false
	}
	fn, ok := builtins[n.Name]
	if !ok {
		return nil, false
	}
	res, err := fn(ctx, n.Args, p.WorkDir)
	if err != nil {
		// A builtin errored at the dispatcher level (not a
		// command-level non-zero exit). Fall through so the real
		// binary can try — keeps parity if our builtin is incomplete.
		return nil, false
	}
	return res, true
}

// ----- builtin implementations -------------------------------------------

func bPwd(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	if len(argv) > 0 {
		// Plain `pwd` only; flags fall through to fork.
		return nil, fmt.Errorf("flags not supported by builtin")
	}
	return &bash.ExecResult{Stdout: workDir + "\n"}, nil
}

func bEcho(_ context.Context, argv []string, _ string) (*bash.ExecResult, error) {
	noNewline := false
	args := argv
	if len(args) > 0 && args[0] == "-n" {
		noNewline = true
		args = args[1:]
	}
	out := strings.Join(args, " ")
	if !noNewline {
		out += "\n"
	}
	return &bash.ExecResult{Stdout: out}, nil
}

func bTrue(_ context.Context, _ []string, _ string) (*bash.ExecResult, error) {
	return &bash.ExecResult{ExitCode: 0}, nil
}

func bFalse(_ context.Context, _ []string, _ string) (*bash.ExecResult, error) {
	return &bash.ExecResult{ExitCode: 1}, nil
}

func bCat(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	if len(argv) == 0 {
		// `cat` with no args reads stdin; we don't intercept that.
		return nil, fmt.Errorf("stdin form not supported by builtin")
	}
	var sb strings.Builder
	for _, p := range argv {
		if strings.HasPrefix(p, "-") {
			return nil, fmt.Errorf("flag %q not supported by builtin", p)
		}
		path := resolveRelative(workDir, p)
		data, err := os.ReadFile(path)
		if err != nil {
			return &bash.ExecResult{
				Stderr:   fmt.Sprintf("cat: %s: %v\n", p, err),
				ExitCode: 1,
			}, nil
		}
		sb.Write(data)
	}
	return &bash.ExecResult{Stdout: sb.String()}, nil
}

func bLs(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	// Recognized flags; anything else → fall through.
	long := false
	all := false
	one := false
	dir := workDir
	pathSet := false
	for _, a := range argv {
		if strings.HasPrefix(a, "-") && a != "-" {
			for _, ch := range a[1:] {
				switch ch {
				case 'l':
					long = true
				case 'a', 'A':
					all = true
				case '1':
					one = true
				default:
					return nil, fmt.Errorf("flag -%c not supported by builtin", ch)
				}
			}
			continue
		}
		if pathSet {
			return nil, fmt.Errorf("multiple paths not supported by builtin")
		}
		dir = resolveRelative(workDir, a)
		pathSet = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &bash.ExecResult{
			Stderr:   fmt.Sprintf("ls: %v\n", err),
			ExitCode: 1,
		}, nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !all && strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	switch {
	case long:
		for _, n := range names {
			info, err := os.Stat(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			fmt.Fprintf(&sb, "%s %d %s %s\n",
				info.Mode().String(),
				info.Size(),
				info.ModTime().Format("Jan _2 15:04"),
				n)
		}
	case one:
		for _, n := range names {
			sb.WriteString(n)
			sb.WriteString("\n")
		}
	default:
		sb.WriteString(strings.Join(names, "  "))
		if len(names) > 0 {
			sb.WriteString("\n")
		}
	}
	return &bash.ExecResult{Stdout: sb.String()}, nil
}

func bMkdir(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	parents := false
	paths := make([]string, 0, len(argv))
	for _, a := range argv {
		switch {
		case a == "-p":
			parents = true
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %q not supported by builtin", a)
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return &bash.ExecResult{
			Stderr:   "mkdir: missing operand\n",
			ExitCode: 1,
		}, nil
	}
	for _, p := range paths {
		path := resolveRelative(workDir, p)
		var err error
		if parents {
			err = os.MkdirAll(path, 0o755)
		} else {
			err = os.Mkdir(path, 0o755)
		}
		if err != nil {
			return &bash.ExecResult{
				Stderr:   fmt.Sprintf("mkdir: %v\n", err),
				ExitCode: 1,
			}, nil
		}
	}
	return &bash.ExecResult{}, nil
}

// bHead/bTail support only the simple `-n N` form.
func bHead(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	n, paths, err := parseHeadTailArgs(argv)
	if err != nil {
		return nil, err
	}
	return readHeadOrTail(paths, workDir, n, true)
}

func bTail(_ context.Context, argv []string, workDir string) (*bash.ExecResult, error) {
	n, paths, err := parseHeadTailArgs(argv)
	if err != nil {
		return nil, err
	}
	return readHeadOrTail(paths, workDir, n, false)
}

func parseHeadTailArgs(argv []string) (int, []string, error) {
	n := 10
	var paths []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-n":
			if i+1 >= len(argv) {
				return 0, nil, fmt.Errorf("-n requires an argument")
			}
			i++
			v, err := strconv.Atoi(argv[i])
			if err != nil {
				return 0, nil, err
			}
			n = v
		case strings.HasPrefix(a, "-n"):
			v, err := strconv.Atoi(a[2:])
			if err != nil {
				return 0, nil, err
			}
			n = v
		case strings.HasPrefix(a, "-"):
			return 0, nil, fmt.Errorf("flag %q not supported by builtin", a)
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return 0, nil, fmt.Errorf("stdin form not supported")
	}
	return n, paths, nil
}

func readHeadOrTail(paths []string, workDir string, n int, head bool) (*bash.ExecResult, error) {
	if len(paths) > 1 {
		return nil, fmt.Errorf("multi-file form not supported by builtin")
	}
	path := resolveRelative(workDir, paths[0])
	data, err := os.ReadFile(path)
	if err != nil {
		return &bash.ExecResult{
			Stderr:   fmt.Sprintf("%s\n", err),
			ExitCode: 1,
		}, nil
	}
	lines := strings.Split(string(data), "\n")
	// strings.Split on a trailing "\n" yields a trailing empty
	// string; drop it so head/tail counts match coreutils.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var slice []string
	if head {
		if n > len(lines) {
			n = len(lines)
		}
		slice = lines[:n]
	} else {
		if n > len(lines) {
			n = len(lines)
		}
		slice = lines[len(lines)-n:]
	}
	out := strings.Join(slice, "\n")
	if len(slice) > 0 {
		out += "\n"
	}
	return &bash.ExecResult{Stdout: out}, nil
}

func bWhich(_ context.Context, argv []string, _ string) (*bash.ExecResult, error) {
	if len(argv) == 0 {
		return &bash.ExecResult{ExitCode: 1}, nil
	}
	var sb strings.Builder
	exit := 0
	for _, name := range argv {
		if strings.HasPrefix(name, "-") {
			return nil, fmt.Errorf("flag %q not supported by builtin", name)
		}
		path, err := lookupPath(name)
		if err != nil {
			exit = 1
			continue
		}
		sb.WriteString(path)
		sb.WriteString("\n")
	}
	return &bash.ExecResult{Stdout: sb.String(), ExitCode: exit}, nil
}

func bBasename(_ context.Context, argv []string, _ string) (*bash.ExecResult, error) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return nil, fmt.Errorf("not supported by builtin")
	}
	out := filepath.Base(argv[0])
	if len(argv) == 2 {
		out = strings.TrimSuffix(out, argv[1])
	}
	return &bash.ExecResult{Stdout: out + "\n"}, nil
}

func bDirname(_ context.Context, argv []string, _ string) (*bash.ExecResult, error) {
	if len(argv) != 1 || strings.HasPrefix(argv[0], "-") {
		return nil, fmt.Errorf("not supported by builtin")
	}
	return &bash.ExecResult{Stdout: filepath.Dir(argv[0]) + "\n"}, nil
}

// ----- helpers ------------------------------------------------------------

// resolveRelative joins workDir with a relative path; absolute
// paths pass through unchanged.
func resolveRelative(workDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if workDir == "" {
		return p
	}
	return filepath.Join(workDir, p)
}

// lookupPath returns the absolute path of name in PATH.
// Equivalent to exec.LookPath but inlined to avoid pulling in
// os/exec just for one lookup.
func lookupPath(name string) (string, error) {
	if strings.Contains(name, "/") {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
		return "", os.ErrNotExist
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
