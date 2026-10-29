//go:build unix

package bashy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// Sprint 531 Story #56 (4e681d8d5e60): genie's `bashy delegate` came back
// timedOut with no envelope because every command was capped at
// execution.timeoutMs (60s) — jobLifetimeMs was only a claim expiry. The
// declared delegation verb bounds itself (--timeout/--idle-timeout), so it is
// ceilinged at min(--timeout + grace, jobLifetimeMs); any other command keeps
// the ordinary ceiling. Scaled down: ceiling 1s, job lifetime 20s.
func delegateExecutor(t *testing.T) *Executor {
	t.Helper()
	t.Setenv("BASHY_HOME", filepath.Join(t.TempDir(), "bashy-home"))
	t.Setenv("BASHY_KB_DIR", filepath.Join(t.TempDir(), "host-kb"))
	t.Setenv("BASHY_SKILLS_DIR", filepath.Join(t.TempDir(), "skills"))
	t.Setenv("YCODE_DATA_DIR", filepath.Join(t.TempDir(), "ycode-data"))
	// `bashy VERB` in the session re-enters BASHY_SELF (under @contain as
	// `bashy contain ... -- bashy delegate ...`): the fake delegate outlives
	// the 1s ceiling, then prints its envelope.
	fake := filepath.Join(t.TempDir(), "bashy")
	script := "#!/bin/sh\nsleep 2\necho '{\"schema_version\":\"bashy-chat-v1\",\"ok\":true}'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASHY_SELF", fake)
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.TimeoutMS, config.JobLifetimeMS = 1000, 20000
	config.Environment = spec.Environment{Inherit: "allowlist", Names: []string{"PATH"}}
	executor, err := NewExecutor(config, cwd, RuntimeOptions{
		ControlRoot:      filepath.Join(root, "control"),
		AuthorizationKey: []byte("0123456789abcdef0123456789abcdef"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func runCall(t *testing.T, executor *Executor, script string) harnessrunner.Result {
	t.Helper()
	value, err := executor.Execute(context.Background(), testMeta(), hitl.Call{ID: "call-1", Name: "bashy", Script: script}, digest("allow"))
	if err != nil {
		t.Fatalf("execute %q: %v", script, err)
	}
	result, ok := value.(harnessrunner.Result)
	if !ok {
		t.Fatalf("execute returned %T", value)
	}
	return result
}

func TestDelegateOutlivesTheCommandCeilingWithinItsOwnTimeout(t *testing.T) {
	executor := delegateExecutor(t)
	// The effects envelope genie's deny remedy teaches for a front-door verb.
	result := runCall(t, executor, "@effects(\"read,write,exec\")\n@contain(net: \"deny\")\nfunction run_checked() {\nbashy delegate --agent genie:other --timeout 2m --idle-timeout 1m 'write the file'\n}\nrun_checked")
	if result.Protocol != "ok" || result.Outcome == harnessrunner.OutcomeTimedOut {
		t.Fatalf("delegate outcome = %q/%q, want it to finish past the 1s ceiling", result.Protocol, result.Outcome)
	}
	// min(2m + grace, 20s job lifetime).
	if result.Intent == nil || result.Intent.Limits.WallTimeMs != 20000 {
		t.Fatalf("delegate wall time = %#v, want the 20s job lifetime", result.Intent)
	}
	if got := string(outputBytes(t, result)); !strings.Contains(got, "bashy-chat-v1") {
		t.Fatalf("delegate stdout = %q, want its envelope", got)
	}
}

func TestNonDelegateCommandIsStillKilledAtTheCeiling(t *testing.T) {
	executor := delegateExecutor(t)
	begin := time.Now()
	result := runCall(t, executor, "sleep 5")
	if result.Outcome != harnessrunner.OutcomeTimedOut || time.Since(begin) > 4*time.Second {
		t.Fatalf("sleep outcome = %q after %v, want timedOut at the 1s ceiling", result.Outcome, time.Since(begin))
	}
	if result.Intent == nil || result.Intent.Limits.WallTimeMs != 1000 {
		t.Fatalf("sleep wall time = %#v, want 1000", result.Intent)
	}
}

func TestDelegateTimeoutMS(t *testing.T) {
	for _, tc := range []struct {
		script string
		ms     int
		ok     bool
	}{
		{"bashy delegate --agent x --timeout 8m task", 480000, true},
		{"bashy delegate --timeout=90s task > out.json 2>&1", 90000, true},
		{"bashy delegate --agent x task", 0, true},
		{"bashy delegate --instruction --timeout --timeout 1s", 1000, true},
		{"bashy delegate -- --timeout 1s", 0, true},
		{"sleep 900", 0, false},
		{"bashy run sleep 900", 0, false},
		{"bashy delegate --timeout 2m x; sleep 900", 0, false},
		{"bashy delegate --timeout 2m x && sleep 900", 0, false},
		{"bashy delegate --timeout 2m x | cat", 0, false},
		{"bashy delegate --timeout 2m x &", 0, false},
		{"bashy delegate --timeout 2m \"$(sleep 900)\"", 0, false},
		{"bashy delegate --timeout 2m <(sleep 900)", 0, false},
		{"X=1 bashy delegate --timeout 2m x", 0, false},
		{"bashy delegate --timeout \"$T\" x", 0, false},
		{"bashy delegate --timeout", 0, false},
		{"$B delegate --timeout 2m x", 0, false},
		{"@effects(\"read,write,exec\")\n@contain(net: \"deny\")\nfunction run_checked() {\nbashy delegate --timeout 8m x\n}\nrun_checked", 480000, true},
		{"@retry(3)\nfunction r() {\nbashy delegate --timeout 8m x\n}\nr", 0, false},
		{"@effects(\"exec\")\nfunction r() {\nbashy delegate --timeout 8m x\nsleep 900\n}\nr", 0, false},
		{"@effects(\"exec\")\nfunction r() {\nsleep 900\n}\nr", 0, false},
		{"@effects(\"exec\")\nfunction r() {\nbashy delegate x\n}\nr; r", 0, false},
		{"@effects(\"exec\")\nfunction r() {\nbashy delegate x\n}\nother", 0, false},
	} {
		ms, ok := delegateTimeoutMS(tc.script)
		if ms != tc.ms || ok != tc.ok {
			t.Errorf("delegateTimeoutMS(%q) = %d,%v want %d,%v", tc.script, ms, ok, tc.ms, tc.ok)
		}
	}
}

func TestWallTimeMS(t *testing.T) {
	e := &Executor{config: spec.Bashy{TimeoutMS: 60000, JobLifetimeMS: 900000}}
	for _, tc := range []struct {
		call Call
		want int
	}{
		{Call{Script: "sleep 900"}, 60000},
		{Call{Script: "bashy delegate --timeout 8m x"}, 480000 + delegateGraceMS},
		{Call{Script: "bashy delegate --timeout 1h x"}, 900000},
		{Call{Script: "bashy delegate x"}, 900000},
		{Call{Script: "bashy delegate x", TimeoutMS: 5000}, 5000},
	} {
		if got := e.wallTimeMS(tc.call); got != tc.want {
			t.Errorf("wallTimeMS(%q) = %d, want %d", tc.call.Script, got, tc.want)
		}
	}
	e.config.JobLifetimeMS = 0
	if got := e.wallTimeMS(Call{Script: "bashy delegate x"}); got != 60000 {
		t.Errorf("without a job lifetime delegate got %d, want 60000", got)
	}
}
