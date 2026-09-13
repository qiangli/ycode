package turn

import (
	"strings"
	"testing"
)

func TestResolveBashyRunScriptSubstitutesSingleQuotedLiterals(t *testing.T) {
	got, err := resolveBashyRunScript("bashy kb context --for {{task}} --episode {{session}}", "session-1", map[string]any{"task": "don't pkill"})
	if err != nil {
		t.Fatal(err)
	}
	want := `bashy kb context --for 'don'\''t pkill' --episode 'session-1'`
	if got != want {
		t.Fatalf("script = %q, want %q", got, want)
	}
}

func TestResolveBashyRunScriptFailsClosed(t *testing.T) {
	if _, err := resolveBashyRunScript("printf {{task}}", "s", nil); err == nil || !strings.Contains(err.Error(), "no bound input") {
		t.Fatalf("err = %v", err)
	}
	if _, err := resolveBashyRunScript("printf {{task}}", "s", map[string]any{"task": map[string]any{"nested": true}}); err == nil || !strings.Contains(err.Error(), "scalar") {
		t.Fatalf("err = %v", err)
	}
}
