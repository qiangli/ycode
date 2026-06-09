package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeOwner(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "local"},
		{"   ", "local"},
		{"alice@example.com", "alice@example.com"},
		{"a.b+tag@c.io", "a.b+tag@c.io"},
		{"bad/../traversal", "bad_.._traversal"},
		{"with spaces", "with_spaces"},
		{strings.Repeat("a", 200), strings.Repeat("a", 128)},
	}
	for _, c := range cases {
		got := sanitizeOwner(c.in)
		if got != c.want {
			t.Errorf("sanitizeOwner(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidID(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"20260608-103000-abc123", true},
		{"cwd", true},
		{"a", true},
		{"", false},
		{"../escape", false},
		{"with/slash", false},
		{"with space", false},
		{strings.Repeat("x", 129), false},
	}
	for _, c := range cases {
		got := validID(c.in)
		if got != c.ok {
			t.Errorf("validID(%q)=%v, want %v", c.in, got, c.ok)
		}
	}
}

func TestResolve_ExplicitWorkDir(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	target := filepath.Join(tmp, "explicit-target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	ws, err := r.Resolve(ResolveHint{ExplicitWorkDir: target, Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != target {
		t.Errorf("path=%s, want %s", ws.Path, target)
	}
	if ws.ID != "explicit" {
		t.Errorf("id=%s, want explicit", ws.ID)
	}
	if ws.Owner != "alice@example.com" {
		t.Errorf("owner=%s, want alice@example.com", ws.Owner)
	}
}

func TestResolve_PolicyCWD(t *testing.T) {
	tmp := t.TempDir()
	cwd := filepath.Join(tmp, "server-cwd")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	r := NewWorkspaceResolver(PolicyCWD, filepath.Join(tmp, "ws"), cwd)
	ws, err := r.Resolve(ResolveHint{})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != cwd {
		t.Errorf("path=%s, want %s", ws.Path, cwd)
	}
	if ws.ID != "cwd" {
		t.Errorf("id=%s, want cwd", ws.ID)
	}
}

func TestResolve_PolicyPerSession_AllocatesUnique(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	a, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Errorf("expected unique ids, got %q twice", a.ID)
	}
	for _, ws := range []Workspace{a, b} {
		st, err := os.Stat(ws.Path)
		if err != nil {
			t.Fatalf("workspace %s not created: %v", ws.Path, err)
		}
		if !st.IsDir() {
			t.Errorf("workspace path is not a dir: %s", ws.Path)
		}
		// Verify it's under <root>/<owner>/.
		want := filepath.Join(tmp, "ws", "alice@example.com", ws.ID)
		if ws.Path != want {
			t.Errorf("path=%s, want %s", ws.Path, want)
		}
	}
}

func TestResolve_Reattach(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	original, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Reattach: pass the ID, no work_dir override.
	reattached, err := r.Resolve(ResolveHint{
		WorkspaceID: original.ID,
		Owner:       "alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reattached.Path != original.Path {
		t.Errorf("reattach path=%s, want %s", reattached.Path, original.Path)
	}
}

func TestResolve_ReattachWrongOwner(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	original, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Bob can't reattach to alice's workspace — the resolver scopes
	// by owner.
	_, err = r.Resolve(ResolveHint{
		WorkspaceID: original.ID,
		Owner:       "bob@example.com",
	})
	if err == nil {
		t.Errorf("expected error reattaching across owner, got nil")
	}
}

func TestList_NewestFirst(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	first, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Touch the mod time of the second so the test isn't flaky.
	_ = os.Chtimes(first.Path, first.CreatedAt, first.CreatedAt)
	second, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := r.List("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d, want 2", len(list))
	}
	if list[0].ID != second.ID {
		t.Errorf("expected newest %q first, got %q (full list: %+v)", second.ID, list[0].ID, list)
	}
}

func TestDelete_RemovesDir(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	ws, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Drop a file inside so RemoveAll has to recurse.
	if err := os.WriteFile(filepath.Join(ws.Path, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("alice@example.com", ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Errorf("workspace dir should be removed, got err=%v", err)
	}
}

func TestDelete_PathTraversalRejected(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyPerSession, filepath.Join(tmp, "ws"), tmp)

	// validID rejects "../..": this is just defense-in-depth.
	if err := r.Delete("alice", "../../etc"); err == nil {
		t.Errorf("expected delete to reject traversal id")
	}
}

func TestResolve_LoomNotWired(t *testing.T) {
	tmp := t.TempDir()
	r := NewWorkspaceResolver(PolicyLoom, filepath.Join(tmp, "ws"), tmp)
	_, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err == nil {
		t.Errorf("expected loom policy to error when no leaser configured")
	}
}

// fakeLeaser is a deterministic LoomLeaser used by the wiring tests.
type fakeLeaser struct {
	calls []string
	id    string
	path  string
	err   error
}

func (f *fakeLeaser) LeaseForOwner(owner string) (string, string, error) {
	f.calls = append(f.calls, owner)
	if f.err != nil {
		return "", "", f.err
	}
	return f.id, f.path, nil
}

func TestResolve_LoomWired_ReturnsLeaserOutput(t *testing.T) {
	tmp := t.TempDir()
	leaser := &fakeLeaser{id: "loom-abc", path: filepath.Join(tmp, "sandbox-abc")}
	r := NewWorkspaceResolver(PolicyLoom, filepath.Join(tmp, "ws"), tmp).WithLoomLeaser(leaser)
	ws, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ws.ID != "loom-abc" {
		t.Errorf("ws.ID=%q want loom-abc", ws.ID)
	}
	if ws.Path != filepath.Join(tmp, "sandbox-abc") {
		t.Errorf("ws.Path=%q want sandbox-abc path", ws.Path)
	}
	if len(leaser.calls) != 1 || leaser.calls[0] != "alice@example.com" {
		t.Errorf("leaser calls=%v", leaser.calls)
	}
}

func TestResolve_LoomWired_PropagatesError(t *testing.T) {
	tmp := t.TempDir()
	leaser := &fakeLeaser{err: fmt.Errorf("backend down")}
	r := NewWorkspaceResolver(PolicyLoom, filepath.Join(tmp, "ws"), tmp).WithLoomLeaser(leaser)
	_, err := r.Resolve(ResolveHint{Owner: "alice@example.com"})
	if err == nil {
		t.Fatal("expected error from leaser to propagate")
	}
	if !strings.Contains(err.Error(), "backend down") {
		t.Errorf("error %v does not wrap leaser error", err)
	}
}
