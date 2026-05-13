package projectid

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name       string
		override   string
		remote     string
		cwd        string
		wantPrefix string // prefix match for cwd-hash branch
		want       string // exact match for explicit and remote branches
	}{
		{
			name:     "explicit override wins",
			override: "my-project",
			remote:   "github.com/foo/bar",
			cwd:      "/tmp/whatever",
			want:     "my-project",
		},
		{
			name:   "normalized remote when no override",
			remote: "github.com/foo/bar",
			cwd:    "/tmp/whatever",
			want:   "github.com/foo/bar",
		},
		{
			name:       "cwd-hash fallback when neither",
			cwd:        "/tmp/whatever",
			wantPrefix: "cwd-hash:",
		},
		{
			name:     "override beats remote even with whitespace",
			override: "  trimmed  ",
			remote:   "github.com/foo/bar",
			want:     "trimmed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.override, tc.remote, tc.cwd)
			if tc.want != "" && got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if tc.wantPrefix != "" && !strings.HasPrefix(got, tc.wantPrefix) {
				t.Fatalf("got %q want prefix %q", got, tc.wantPrefix)
			}
		})
	}
}

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/foo/bar.git", "github.com/foo/bar"},
		{"https://user:tok@github.com/foo/bar.git", "github.com/foo/bar"},
		{"git@github.com:foo/bar.git", "github.com/foo/bar"},
		{"ssh://git@gitlab.example.com:2222/x/y", "gitlab.example.com:2222/x/y"},
		{"/local/path/repo", "/local/path/repo"},
		{"", ""},
		{"  https://github.com/foo/bar  ", "github.com/foo/bar"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := NormalizeRemote(tc.in)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeRoundTrip(t *testing.T) {
	ids := []string{
		"github.com/foo/bar",
		"gitlab.example.com:2222/x/y",
		"cwd-hash:abcd1234",
		"my-explicit-id",
		"weird/with:both",
	}
	seen := make(map[string]string)
	for _, id := range ids {
		s := Sanitize(id)
		if strings.ContainsAny(s, `/\:`) {
			t.Errorf("Sanitize(%q) = %q still contains unsafe chars", id, s)
		}
		if prev, ok := seen[s]; ok && prev != id {
			t.Errorf("Sanitize collision: %q and %q both → %q", prev, id, s)
		}
		seen[s] = id
	}
}

func TestSanitizeKnownOutputs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/foo/bar", "github.com%2Ffoo%2Fbar"},
		{"cwd-hash:abcd1234", "cwd-hash%3Aabcd1234"},
		{"plain", "plain"},
	}
	for _, tc := range cases {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("Sanitize(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestTwoCheckoutsConverge initializes two temp dirs as git repos
// pointing at the same fake remote URL and asserts that
// ResolveFromCwd returns the same project id for both — which means
// StateDir places their backlog and foreman state in the same
// directory. This is the contract that lets two clones of one repo
// share state.
func TestTwoCheckoutsConverge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	a := t.TempDir()
	b := t.TempDir()
	remote := "https://github.com/example/shared-repo.git"

	for _, dir := range []string{a, b} {
		mustRun(t, dir, "git", "init", "-q")
		mustRun(t, dir, "git", "remote", "add", "origin", remote)
	}

	idA := ResolveFromCwd(ctx, a, "")
	idB := ResolveFromCwd(ctx, b, "")
	if idA == "" || idB == "" {
		t.Fatalf("empty id: a=%q b=%q (git remote may have been intercepted by a PATH shim)", idA, idB)
	}
	if idA != idB {
		t.Fatalf("two checkouts of %s diverged: a=%q b=%q", remote, idA, idB)
	}
	if !strings.HasPrefix(idA, "github.com/example/shared-repo") {
		t.Errorf("id does not look normalized: %q", idA)
	}

	// Distinct logical repos must NOT converge.
	c := t.TempDir()
	mustRun(t, c, "git", "init", "-q")
	mustRun(t, c, "git", "remote", "add", "origin", "https://github.com/example/different.git")
	idC := ResolveFromCwd(ctx, c, "")
	if idC == idA {
		t.Fatalf("different remotes should produce different ids: %q == %q", idC, idA)
	}

	// Explicit override beats everything.
	idOverride := ResolveFromCwd(ctx, a, "my-explicit-id")
	if idOverride != "my-explicit-id" {
		t.Fatalf("override ignored: got %q", idOverride)
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
}

func TestStateDirComposition(t *testing.T) {
	got := StateDir("/home/u/.agents/ycode", "github.com/foo/bar")
	want := filepath.Join("/home/u/.agents/ycode", "projects", "github.com%2Ffoo%2Fbar")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if BacklogDir(got) != filepath.Join(want, "backlog") {
		t.Fatal("BacklogDir composition wrong")
	}
	if ForemanDir(got) != filepath.Join(want, "foreman") {
		t.Fatal("ForemanDir composition wrong")
	}
	if ProjectSettingsPath(got) != filepath.Join(want, "settings.json") {
		t.Fatal("ProjectSettingsPath composition wrong")
	}
}
