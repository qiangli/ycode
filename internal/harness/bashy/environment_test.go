package bashy

import (
	"os"
	"path/filepath"
	"testing"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"
)

func testExecutorWithNames(t *testing.T, names []string) *Executor {
	t.Helper()
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.Environment.Names = names
	executor, err := NewExecutor(config, cwd, RuntimeOptions{
		ControlRoot:      filepath.Join(root, "control"),
		AuthorizationKey: []byte("0123456789abcdef0123456789abcdef"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func envValue(vars []harnessrunner.EnvironmentVariable, name string) (string, bool) {
	for _, v := range vars {
		if v.Name == name {
			return v.Value, true
		}
	}
	return "", false
}

// Sprint 379 Story #51 (a87716307f79): a headless genie worker's commands
// carried a bashy-hint-v1 JSON line on stderr in every observation because
// nothing forced BASHY_HINTS off for the executed command when the ycode
// process itself did not happen to carry the var. The allowlist alone let a
// declared-but-unset advisory var silently vanish instead of defaulting to
// quiet, the one posture that makes sense for a model-visible tool call.
func TestEnvironmentDefaultsHintAdvisoryAndTelemetryVarsToQuietWhenUnset(t *testing.T) {
	for _, name := range []string{"BASHY_HINTS", "BASHY_ADVISOR", "BASHY_TELEMETRY_QUIET"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	executor := testExecutorWithNames(t, []string{"BASHY_HINTS", "BASHY_ADVISOR", "BASHY_TELEMETRY_QUIET"})
	vars := executor.environment()
	for name, want := range quietDefaults {
		got, ok := envValue(vars, name)
		if !ok || got != want {
			t.Fatalf("%s = %q, %v; want quiet default %q", name, got, ok, want)
		}
	}
}

// A caller that explicitly turns hints back on (an ablation run) is still
// respected: the quiet default only fills a gap, it never overrides.
func TestEnvironmentRespectsAnExplicitlySetHintVar(t *testing.T) {
	t.Setenv("BASHY_HINTS", "on")
	executor := testExecutorWithNames(t, []string{"BASHY_HINTS"})
	got, ok := envValue(executor.environment(), "BASHY_HINTS")
	if !ok || got != "on" {
		t.Fatalf("BASHY_HINTS = %q, %v; want the explicit host value preserved", got, ok)
	}
}

// A name outside the quiet-defaults set with no host value stays omitted:
// the default only applies to the known advisory/telemetry controls.
func TestEnvironmentLeavesOtherUnsetAllowlistedNamesOmitted(t *testing.T) {
	t.Setenv("HARNESS_UNSET_VALUE", "")
	_ = os.Unsetenv("HARNESS_UNSET_VALUE")
	executor := testExecutorWithNames(t, []string{"HARNESS_UNSET_VALUE"})
	if _, ok := envValue(executor.environment(), "HARNESS_UNSET_VALUE"); ok {
		t.Fatal("an unset, non-advisory allowlisted name should stay omitted")
	}
}

// The var is forwarded at all only when the compiled allowlist declares it:
// the mechanism never adds model-visible environment the YAML did not ask for.
func TestEnvironmentNeverForwardsAnUndeclaredQuietDefault(t *testing.T) {
	for _, name := range []string{"BASHY_HINTS", "BASHY_ADVISOR", "BASHY_TELEMETRY_QUIET"} {
		_ = os.Unsetenv(name)
	}
	executor := testExecutorWithNames(t, nil)
	for name := range quietDefaults {
		if _, ok := envValue(executor.environment(), name); ok {
			t.Fatalf("%s forwarded without being in the compiled allowlist", name)
		}
	}
}
