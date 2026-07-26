package agentswitch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBashy puts an executable named bashy on PATH so Command() can resolve a
// binary without one being installed.
func fakeBashy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bashy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(BinEnv, "")
	return bin
}

func argvString(argv []string) string { return strings.Join(argv, " ") }

func TestCommandBuildsInteractiveAgentSwitch(t *testing.T) {
	fakeBashy(t)

	argv, target, err := Command(Request{Agent: "L4", Interactive: true, Context: "we were fixing the retry budget"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	got := argvString(argv)
	for _, want := range []string{"chat", "--agent", "-i", "--context", "we were fixing the retry budget"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv missing %q: %s", want, got)
		}
	}
	if target.Agent.Name == "" {
		t.Error("target agent not resolved")
	}
	// An interactive switch must not carry -m: that would make bashy run a
	// one-shot instead of opening a session.
	if strings.Contains(got, " -m ") {
		t.Errorf("interactive switch should not pass -m: %s", got)
	}
}

// Carrying context is the DEFAULT. If it were opt-in, switching through ycode
// would be strictly worse than opening a terminal.
func TestCarryIsTheDefaultMode(t *testing.T) {
	fakeBashy(t)

	argv, _, err := Command(Request{Agent: "L4", Interactive: true, Context: "prior work"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !strings.Contains(argvString(argv), "--context prior work") {
		t.Errorf("default mode did not carry context: %s", argvString(argv))
	}
}

func TestFreshModeDropsContextAndFiles(t *testing.T) {
	fakeBashy(t)

	argv, _, err := Command(Request{
		Agent: "L4", Interactive: true, Mode: ModeFresh,
		Context: "should not appear", Files: []string{"/tmp/x.md"},
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	got := argvString(argv)
	if strings.Contains(got, "--context") || strings.Contains(got, "--file") {
		t.Errorf("fresh mode leaked context: %s", got)
	}
}

func TestHeadlessSwitchRequiresABrief(t *testing.T) {
	fakeBashy(t)

	if _, _, err := Command(Request{Agent: "L4"}); err == nil {
		t.Fatal("a headless switch with no instruction should fail — bashy would open a session nobody is attached to")
	}

	argv, _, err := Command(Request{Agent: "L4", Instruction: "summarize the diff", Timeout: 90 * time.Second})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	got := argvString(argv)
	if !strings.Contains(got, "-m summarize the diff") {
		t.Errorf("headless run missing the instruction: %s", got)
	}
	if !strings.Contains(got, "--timeout 1m30s") {
		t.Errorf("headless run missing the timeout: %s", got)
	}
	if strings.Contains(got, " -i") {
		t.Errorf("headless run must not be interactive: %s", got)
	}
}

func TestSwitchByTool(t *testing.T) {
	fakeBashy(t)

	argv, target, err := Command(Request{Tool: "ycode", Interactive: true})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !strings.Contains(argvString(argv), "--tool ycode") {
		t.Errorf("argv did not select the tool: %s", argvString(argv))
	}
	if target.Tool != "ycode" {
		t.Errorf("target.Tool = %q", target.Tool)
	}
}

func TestResolveRejectsAmbiguousOrEmptySelection(t *testing.T) {
	for _, r := range []Request{
		{},
		{Agent: "L4", Tool: "codex"},
		{Agent: "   "},
	} {
		if _, err := Resolve(r); err == nil {
			t.Errorf("Resolve(%+v) succeeded; want an error", r)
		}
	}
}

// ReadOnly propagates DOWN but is never widened here — bashy owns the child's
// governance, and synthesizing --yolo from ycode would route around it.
func TestReadOnlyPropagatesAndNothingIsWidened(t *testing.T) {
	fakeBashy(t)

	argv, _, err := Command(Request{Agent: "L4", Interactive: true, ReadOnly: true})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	got := argvString(argv)
	if !strings.Contains(got, "--read-only") {
		t.Errorf("read-only did not propagate: %s", got)
	}
	for _, forbidden := range []string{"--yolo", "--allow-premium", "--dangerously", "--sandbox"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("ycode must not widen the child's governance (%s): %s", forbidden, got)
		}
	}
}

func TestGuardRefusesNesting(t *testing.T) {
	fakeBashy(t)

	t.Setenv(DepthEnv, "1")
	err := Guard()
	if err == nil {
		t.Fatal("Guard allowed a nested switch")
	}
	if !strings.Contains(err.Error(), DepthEnv) {
		t.Errorf("error should name the guard: %v", err)
	}
}

func TestGuardRefusesInsideAMeeting(t *testing.T) {
	fakeBashy(t)

	t.Setenv(DepthEnv, "0")
	t.Setenv(MeetDepthEnv, "1")
	if err := Guard(); err == nil {
		t.Fatal("Guard allowed a switch from inside a meeting")
	}
}

func TestGuardPassesWhenClear(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("switching is refused on windows by design")
	}
	fakeBashy(t)
	t.Setenv(DepthEnv, "")
	t.Setenv(MeetDepthEnv, "")

	if err := Guard(); err != nil {
		t.Fatalf("Guard refused a clear session: %v", err)
	}
}

// The depth stamp must go to the CHILD. Setting it with os.Setenv would leak
// the guard into every later tool call in this session.
func TestChildEnvIncrementsDepth(t *testing.T) {
	t.Setenv(DepthEnv, "0")
	if got := ChildEnv(); len(got) != 1 || got[0] != DepthEnv+"=1" {
		t.Errorf("ChildEnv() = %v, want [%s=1]", got, DepthEnv)
	}
	t.Setenv(DepthEnv, "1")
	if got := ChildEnv(); got[0] != DepthEnv+"=2" {
		t.Errorf("ChildEnv() = %v, want [%s=2]", got, DepthEnv)
	}
}

// Absence of bashy must be a named error, never a silent fallback to running
// the tool directly — that would bypass resolution, sandboxing and the
// credential firewall.
func TestBashyPathMissingIsALoudError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(BinEnv, "")
	t.Setenv("DHNT_BIN_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	_, err := BashyPath()
	if err == nil {
		t.Fatal("expected an error when bashy is absent")
	}
	if !strings.Contains(err.Error(), BinEnv) {
		t.Errorf("error should name the override: %v", err)
	}
}

func TestBashyPathHonorsOverride(t *testing.T) {
	bin := fakeBashy(t)
	t.Setenv(BinEnv, bin)

	got, err := BashyPath()
	if err != nil {
		t.Fatalf("BashyPath: %v", err)
	}
	if got != bin {
		t.Errorf("BashyPath() = %q, want %q", got, bin)
	}
}
