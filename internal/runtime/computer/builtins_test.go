package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

func runBuiltin(t *testing.T, cmd string, workDir string) (*bash.ExecResult, bool) {
	t.Helper()
	return tryBuiltin(context.Background(), bash.ExecParams{Command: cmd, WorkDir: workDir})
}

func TestBuiltin_Pwd(t *testing.T) {
	res, ok := runBuiltin(t, "pwd", "/wd")
	if !ok {
		t.Fatal("pwd should dispatch as builtin")
	}
	if res.Stdout != "/wd\n" {
		t.Errorf("Stdout = %q, want \"/wd\\n\"", res.Stdout)
	}
}

func TestBuiltin_Echo(t *testing.T) {
	res, ok := runBuiltin(t, "echo hello world", "")
	if !ok {
		t.Fatal("echo should dispatch as builtin")
	}
	if res.Stdout != "hello world\n" {
		t.Errorf("Stdout = %q", res.Stdout)
	}

	res, ok = runBuiltin(t, "echo -n no-newline", "")
	if !ok {
		t.Fatal("echo -n should dispatch as builtin")
	}
	if res.Stdout != "no-newline" {
		t.Errorf("Stdout = %q", res.Stdout)
	}
}

func TestBuiltin_TrueFalse(t *testing.T) {
	res, _ := runBuiltin(t, "true", "")
	if res.ExitCode != 0 {
		t.Errorf("true exit = %d, want 0", res.ExitCode)
	}
	res, _ = runBuiltin(t, "false", "")
	if res.ExitCode != 1 {
		t.Errorf("false exit = %d, want 1", res.ExitCode)
	}
	res, _ = runBuiltin(t, ":", "")
	if res.ExitCode != 0 {
		t.Errorf(": exit = %d, want 0", res.ExitCode)
	}
}

func TestBuiltin_Cat(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	_ = os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644)
	res, ok := runBuiltin(t, "cat f.txt", tmp)
	if !ok {
		t.Fatal("cat should dispatch as builtin")
	}
	if res.Stdout != "alpha\nbeta\n" {
		t.Errorf("Stdout = %q", res.Stdout)
	}
}

func TestBuiltin_Ls(t *testing.T) {
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "a"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "b"), nil, 0o644)
	res, ok := runBuiltin(t, "ls", tmp)
	if !ok {
		t.Fatal("ls should dispatch as builtin")
	}
	if !strings.Contains(res.Stdout, "a") || !strings.Contains(res.Stdout, "b") {
		t.Errorf("Stdout = %q", res.Stdout)
	}
}

func TestBuiltin_Mkdir(t *testing.T) {
	tmp := t.TempDir()
	res, ok := runBuiltin(t, "mkdir sub", tmp)
	if !ok {
		t.Fatal("mkdir should dispatch as builtin")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	info, err := os.Stat(filepath.Join(tmp, "sub"))
	if err != nil || !info.IsDir() {
		t.Errorf("sub dir not created: %v", err)
	}
}

func TestBuiltin_Mkdir_P(t *testing.T) {
	tmp := t.TempDir()
	res, ok := runBuiltin(t, "mkdir -p a/b/c", tmp)
	if !ok || res.ExitCode != 0 {
		t.Fatalf("ok=%v, res=%+v", ok, res)
	}
	info, err := os.Stat(filepath.Join(tmp, "a", "b", "c"))
	if err != nil || !info.IsDir() {
		t.Errorf("nested dir not created: %v", err)
	}
}

func TestBuiltin_HeadTail(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "lines.txt")
	_ = os.WriteFile(path, []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n"), 0o644)

	res, _ := runBuiltin(t, "head -n 3 lines.txt", tmp)
	if res.Stdout != "1\n2\n3\n" {
		t.Errorf("head Stdout = %q", res.Stdout)
	}
	res, _ = runBuiltin(t, "tail -n 2 lines.txt", tmp)
	if res.Stdout != "10\n11\n" {
		t.Errorf("tail Stdout = %q", res.Stdout)
	}
}

func TestBuiltin_Basename_Dirname(t *testing.T) {
	res, _ := runBuiltin(t, "basename /a/b/c.txt", "")
	if res.Stdout != "c.txt\n" {
		t.Errorf("basename = %q", res.Stdout)
	}
	res, _ = runBuiltin(t, "dirname /a/b/c.txt", "")
	if res.Stdout != "/a/b\n" {
		t.Errorf("dirname = %q", res.Stdout)
	}
}

func TestBuiltin_FallsThroughOnPipeline(t *testing.T) {
	// Pipelines must NOT dispatch as builtin even though `echo` is a
	// builtin — the second stage requires real shell semantics.
	if _, ok := runBuiltin(t, "echo hi | wc -l", ""); ok {
		t.Error("pipeline should fall through")
	}
}

func TestBuiltin_FallsThroughOnRedirect(t *testing.T) {
	if _, ok := runBuiltin(t, "echo hi > /tmp/x", ""); ok {
		t.Error("redirect should fall through")
	}
}

func TestBuiltin_FallsThroughOnAndChain(t *testing.T) {
	// `&&` produces multiple CommandNodes — len != 1 short-circuits.
	if _, ok := runBuiltin(t, "true && echo hi", ""); ok {
		t.Error("&& chain should fall through")
	}
}

func TestBuiltin_FallsThroughOnUnknown(t *testing.T) {
	if _, ok := runBuiltin(t, "fakebinary 1 2 3", ""); ok {
		t.Error("unknown binary should fall through")
	}
}

func TestBuiltin_FallsThroughOnUnsupportedFlag(t *testing.T) {
	// `ls -X` (sort by extension) is not in our supported flag set.
	if _, ok := runBuiltin(t, "ls -X", ""); ok {
		t.Error("ls -X should fall through")
	}
}

func TestShellRun_BuiltinFastPath(t *testing.T) {
	rec := installRecorder(t)
	c, _ := newTestComputer(t)
	res, err := c.Shell().Run(context.Background(), bash.ExecParams{
		Command: "pwd",
		WorkDir: "/abc",
	})
	if err != nil || res.Stdout != "/abc\n" {
		t.Fatalf("Run: res=%+v err=%v", res, err)
	}

	// Find the shell.run span and verify forked=false.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.spans) == 0 {
		t.Fatal("no spans")
	}
	for _, s := range rec.spans {
		if s.Name() == "ycode.computer.shell.run" {
			seenForked := false
			for _, kv := range s.Attributes() {
				if kv.Key == AttrForked {
					seenForked = true
					if kv.Value.AsBool() != false {
						t.Errorf("expected forked=false, got %v", kv.Value)
					}
				}
			}
			if !seenForked {
				t.Error("forked attribute missing")
			}
			return
		}
	}
	t.Fatal("ycode.computer.shell.run span not found")
}
