package agentswitch

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The argv we build must be argv the REAL bashy accepts.
//
// Unit tests only prove Command() agrees with itself. This asserts it agrees
// with the CLI it targets, which is the contract that actually breaks: bashy's
// flags live in another repo and move independently. It has already earned its
// place once — it caught the brief being passed as both --context and -m.
//
// Skips when bashy is absent, so it costs nothing in a bare CI container.
//
// One property of the validator to keep in mind: --dry-run forces bashy's
// NON-interactive path, which then requires an instruction. Every case below
// therefore carries context or an instruction; that is a constraint of the
// probe, not of the flags under test.
func TestArgvIsAcceptedByRealBashy(t *testing.T) {
	if _, err := BashyPath(); err != nil {
		t.Skip("bashy not installed; skipping the argv contract check")
	}

	cases := []struct {
		name string
		req  Request
	}{
		{"interactive agent", Request{Agent: "L4", Interactive: true, Context: "prior context"}},
		{"interactive tool", Request{Tool: "codex", Interactive: true, Context: "prior context"}},
		{"interactive read-only", Request{Agent: "L4", Interactive: true, ReadOnly: true, Context: "prior context"}},
		{"headless with timeout", Request{Agent: "L4", Instruction: "do the thing", Timeout: 90 * time.Second}},
		{"headless carrying context", Request{Agent: "L4", Instruction: "do the thing", Context: "prior context"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, _, err := Command(tc.req)
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			out, err := exec.Command(argv[0], append(argv[1:], "--dry-run")...).CombinedOutput()
			if err != nil {
				t.Fatalf("bashy rejected our argv:\n  %s\n  %s",
					strings.Join(argv[1:], " "), strings.TrimSpace(string(out)))
			}
		})
	}
}

// The instruction and the carried context are distinct flags; sending one
// string as both duplicates the payload on the wire.
func TestInstructionAndContextAreNotConflated(t *testing.T) {
	fakeBashy(t)

	argv, _, err := Command(Request{Agent: "L4", Instruction: "do X", Context: "we discussed Y"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	got := argvString(argv)
	if !strings.Contains(got, "-m do X") {
		t.Errorf("instruction missing: %s", got)
	}
	if !strings.Contains(got, "--context we discussed Y") {
		t.Errorf("context missing: %s", got)
	}
	if strings.Count(got, "do X") != 1 {
		t.Errorf("instruction sent more than once: %s", got)
	}
}
