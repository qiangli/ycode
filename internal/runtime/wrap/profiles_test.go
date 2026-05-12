package wrap

import (
	"reflect"
	"testing"
)

func TestResolveProfile_ExplicitName(t *testing.T) {
	p, ok := ResolveProfile("aider", nil)
	if !ok {
		t.Fatal("aider profile not found by name")
	}
	if p.Name != "aider" {
		t.Errorf("got Name=%q, want aider", p.Name)
	}
}

func TestResolveProfile_ExplicitUnknown(t *testing.T) {
	_, ok := ResolveProfile("not-a-real-agent", nil)
	if ok {
		t.Errorf("ResolveProfile returned ok=true for unknown name")
	}
}

func TestResolveProfile_AutodetectByBasename(t *testing.T) {
	cases := []struct {
		argv0       string
		wantProfile string
	}{
		{"claude", "claude"},
		{"claude-code", "claude"},
		{"/usr/local/bin/aider", "aider"},
		{"opencode", "opencode"},
		{"gemini-cli", "gemini"},
		{"gemini", "gemini"},
		{"codex", "codex"},
		{"OPENCODE.EXE", "opencode"}, // Windows naming + uppercase
		{"some-random-binary", ""},   // no match
		{"", ""},                     // empty argv
	}
	for _, tc := range cases {
		args := []string{tc.argv0}
		if tc.argv0 == "" {
			args = nil
		}
		p, ok := ResolveProfile("", args)
		if tc.wantProfile == "" {
			if ok {
				t.Errorf("argv0=%q: expected no match, got %q", tc.argv0, p.Name)
			}
			continue
		}
		if !ok {
			t.Errorf("argv0=%q: expected match for %q, got none", tc.argv0, tc.wantProfile)
			continue
		}
		if p.Name != tc.wantProfile {
			t.Errorf("argv0=%q: matched %q, want %q", tc.argv0, p.Name, tc.wantProfile)
		}
	}
}

func TestProfile_Apply_FillsEmptyFieldsOnly(t *testing.T) {
	// Caller already set Permission; profile must not overwrite.
	opts := Options{Permission: "read-only"}
	p := AgentProfiles["aider"]
	p.Apply(&opts)

	if opts.Permission != "read-only" {
		t.Errorf("Apply overwrote Permission: %q", opts.Permission)
	}
	// ExtraShims should now contain the aider list.
	if !contains(opts.ExtraShims, "ruff") {
		t.Errorf("Apply did not add ruff: %v", opts.ExtraShims)
	}
	// RuntimeHooks should be filled because caller left it empty.
	if !reflect.DeepEqual(opts.RuntimeHooks, []string{"python"}) {
		t.Errorf("RuntimeHooks not set from profile: %v", opts.RuntimeHooks)
	}
}

func TestProfile_Apply_CLIRuntimeHooksWin(t *testing.T) {
	// Caller explicitly set RuntimeHooks; profile must not overwrite.
	opts := Options{RuntimeHooks: []string{"node"}}
	p := AgentProfiles["aider"] // profile says python
	p.Apply(&opts)

	if !reflect.DeepEqual(opts.RuntimeHooks, []string{"node"}) {
		t.Errorf("CLI RuntimeHooks was overwritten: %v", opts.RuntimeHooks)
	}
}

func TestProfile_Apply_ShimMergeDedups(t *testing.T) {
	// User passed ruff via --shim; profile also lists ruff. Result must
	// contain a single ruff entry.
	opts := Options{ExtraShims: []string{"ruff", "my-custom-tool"}}
	p := AgentProfiles["aider"]
	p.Apply(&opts)

	count := 0
	for _, s := range opts.ExtraShims {
		if s == "ruff" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ruff appeared %d times in merged shims: %v", count, opts.ExtraShims)
	}
	if !contains(opts.ExtraShims, "my-custom-tool") {
		t.Errorf("user --shim entry lost: %v", opts.ExtraShims)
	}
}

func TestProfile_Apply_NilSafe(t *testing.T) {
	var p *Profile
	opts := Options{}
	// Must not panic.
	p.Apply(&opts)
	p2 := AgentProfiles["aider"]
	p2.Apply(nil) // must not panic
}

func TestProfileNames_Sorted(t *testing.T) {
	got := ProfileNames()
	want := []string{"aider", "claude", "codex", "gemini", "opencode"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ProfileNames = %v, want %v", got, want)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
