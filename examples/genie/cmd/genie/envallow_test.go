package main

// Sprint: #322; Story: #1133; Story-ID: 6205edd11e53
//
// The bashy tool runs with only the allowlisted environment. A SWE-bench task
// image activates its conda env `testbed`; the activation must reach the tool,
// because bashy's agent-mode python/pip shims use the activated environment
// only when VIRTUAL_ENV or CONDA_PREFIX is set. Without them every test genie
// ran became `bashy python …`, which tried to download uv with no network.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBashyToolEnvironmentKeepsPythonActivation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)inherit:\s*allowlist.*?names:\s*\[([^\]]*)\]`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("agent.yaml: no environment allowlist")
	}
	names := map[string]bool{}
	for name := range strings.SplitSeq(string(m[1]), ",") {
		names[strings.TrimSpace(name)] = true
	}
	for _, want := range []string{"VIRTUAL_ENV", "CONDA_PREFIX", "CONDA_DEFAULT_ENV"} {
		if !names[want] {
			t.Errorf("bashy tool environment drops %s: an activated environment never reaches the tool", want)
		}
	}
}
