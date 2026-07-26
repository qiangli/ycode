package commands

import (
	"context"
	"strings"
	"testing"
)

func TestParseSwitchArgs(t *testing.T) {
	cases := []struct {
		in       string
		selector string
		fresh    bool
		takeover bool
		wantErr  bool
	}{
		{"", "", false, false, false},
		{"codex-gpt-5.5", "codex-gpt-5.5", false, false, false},
		{"  L3  ", "L3", false, false, false},
		{"codex --fresh", "codex", true, false, false},
		{"--fresh codex", "codex", true, false, false},
		{"codex --no-context", "codex", true, false, false},
		{"codex --takeover", "codex", false, true, false},
		{"codex --fresh --takeover", "codex", true, true, false},
		{"codex --bogus", "", false, false, true},
		{"codex extra", "", false, false, true},
	}
	for _, tc := range cases {
		sel, fresh, takeover, err := parseSwitchArgs(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseSwitchArgs(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if sel != tc.selector || fresh != tc.fresh || takeover != tc.takeover {
			t.Errorf("parseSwitchArgs(%q) = (%q, fresh=%v, takeover=%v), want (%q, fresh=%v, takeover=%v)",
				tc.in, sel, fresh, takeover, tc.selector, tc.fresh, tc.takeover)
		}
	}
}

// An unknown flag must NOT be swallowed into the selector: `/agent codex
// --frsh` has to complain, or the user silently loses their context while
// believing they asked to drop it.
func TestUnknownFlagIsRejectedNotTreatedAsASelector(t *testing.T) {
	_, _, _, err := parseSwitchArgs("codex --frsh")
	if err == nil {
		t.Fatal("a misspelled flag was accepted")
	}
	if !strings.Contains(err.Error(), "--fresh") {
		t.Errorf("error should name the supported flag: %v", err)
	}
}

// With no selector the command LISTS. That path is pure, so it works
// everywhere — including where switching itself cannot.
func TestSwitchWithNoSelectorListsWithoutASwitcher(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{}) // no AgentSwitcher

	for _, name := range []string{"agent", "tool"} {
		out, err := r.Execute(context.Background(), name, "")
		if err != nil {
			t.Fatalf("/%s with no args: %v", name, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("/%s listed nothing", name)
		}
		if !strings.Contains(out, "/"+name+" <") {
			t.Errorf("/%s did not show its usage: %s", name, out)
		}
	}
}

// Without a switcher, naming a target must report the equivalent direct
// command rather than failing blankly or pretending to switch.
func TestSwitchWithoutASwitcherPointsAtTheDirectCommand(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{})

	_, err := r.Execute(context.Background(), "agent", "codex-gpt-5.5")
	if err == nil {
		t.Fatal("expected an error with no AgentSwitcher")
	}
	if !strings.Contains(err.Error(), "bashy chat") {
		t.Errorf("error should name the direct command: %v", err)
	}
}

func TestSwitchPassesTheRequestThrough(t *testing.T) {
	var got SwitchRequest
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{
		AgentSwitcher: func(_ context.Context, req SwitchRequest) (string, error) {
			got = req
			return "switched", nil
		},
	})

	if _, err := r.Execute(context.Background(), "agent", "codex --fresh"); err != nil {
		t.Fatalf("/agent: %v", err)
	}
	if got.Agent != "codex" || got.Tool != "" || !got.Fresh || got.Verb != "agent" {
		t.Errorf("/agent produced %+v", got)
	}

	if _, err := r.Execute(context.Background(), "tool", "codex"); err != nil {
		t.Fatalf("/tool: %v", err)
	}
	if got.Tool != "codex" || got.Agent != "" || got.Fresh || got.Verb != "tool" {
		t.Errorf("/tool produced %+v", got)
	}
}

// Carrying context is the default; --fresh is the only way to opt out.
func TestContextIsCarriedUnlessFreshIsAsked(t *testing.T) {
	var got SwitchRequest
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{
		AgentSwitcher: func(_ context.Context, req SwitchRequest) (string, error) {
			got = req
			return "", nil
		},
	})
	if _, err := r.Execute(context.Background(), "agent", "codex"); err != nil {
		t.Fatal(err)
	}
	if got.Fresh {
		t.Error("context was dropped without --fresh being asked for")
	}
}

func TestRenderHandoffEmptySession(t *testing.T) {
	if got := RenderHandoff(nil, "/tmp"); got != "" {
		t.Errorf("nil session rendered %q, want empty", got)
	}
}

// ATTACH IS THE DEFAULT and takeover is opt-in. Getting this backwards would
// silently change what switching MEANS: the user would be ejected from ycode
// when they expected its replies to appear in place.
func TestAttachIsTheDefaultAndTakeoverIsOptIn(t *testing.T) {
	var got SwitchRequest
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{
		AgentSwitcher: func(_ context.Context, req SwitchRequest) (string, error) {
			got = req
			return "", nil
		},
	})

	if _, err := r.Execute(context.Background(), "agent", "codex"); err != nil {
		t.Fatal(err)
	}
	if got.Takeover {
		t.Error("a plain /agent asked for takeover; attach must be the default")
	}

	if _, err := r.Execute(context.Background(), "agent", "codex --takeover"); err != nil {
		t.Fatal(err)
	}
	if !got.Takeover {
		t.Error("--takeover did not reach the switcher")
	}
}

func TestDetachWithoutADetacherFailsClearly(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{})

	_, err := r.Execute(context.Background(), "detach", "")
	if err == nil {
		t.Fatal("/detach with nothing attached should error, not claim success")
	}
}

func TestDetachReachesTheDetacher(t *testing.T) {
	called := false
	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{
		Detacher: func() (string, error) { called = true; return "detached", nil },
	})

	out, err := r.Execute(context.Background(), "detach", "")
	if err != nil {
		t.Fatal(err)
	}
	if !called || out != "detached" {
		t.Errorf("/detach did not reach the detacher (called=%v, out=%q)", called, out)
	}
}
