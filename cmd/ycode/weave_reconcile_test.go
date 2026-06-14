package main

import (
	"os/exec"
	"strings"
	"testing"
)

// gitT runs a git command in dir, failing the test on error.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir,
		"-c", "user.email=t@t", "-c", "user.name=t",
		"-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupMergeFixture builds a user repo (main + seed) and a sandbox clone
// with one extra commit, returning (root, sandbox, sandboxHeadSha). The
// sandbox commit is NOT yet present in root — the caller decides whether
// to merge it, exercising both arms of weaveItemMerged.
func setupMergeFixture(t *testing.T) (root, sandbox, sha string) {
	t.Helper()
	root = t.TempDir()
	gitT(t, root, "init", "-q", "-b", "main")
	gitT(t, root, "commit", "--allow-empty", "-qm", "seed")

	sandbox = t.TempDir()
	gitT(t, sandbox, "clone", "-q", root, ".")
	gitT(t, sandbox, "checkout", "-q", "-b", "agent/weave-issue-1")
	gitT(t, sandbox, "commit", "--allow-empty", "-qm", "agent work")
	sha = gitT(t, sandbox, "rev-parse", "HEAD")
	return root, sandbox, sha
}

func TestWeaveItemMerged(t *testing.T) {
	root, sandbox, sha := setupMergeFixture(t)

	// Before merge: the agent commit lives only in the sandbox clone, so
	// the sha is not reachable from main in root — not merged. This is
	// the exact bug case `git branch -d` could never detect (the branch
	// was never fetched into the user repo).
	it := &weaveItem{State: "submitted", Head: sha, Sandbox: sandbox, Branch: "agent/weave-issue-1", CommitsAhead: 1}
	if weaveItemMerged(root, "main", it) {
		t.Fatalf("expected not-merged before the sandbox commit lands in main")
	}

	// Merge the agent branch into root's main (simulating an out-of-band
	// merge / a prior `weave pull`).
	gitT(t, root, "fetch", "-q", sandbox, "agent/weave-issue-1:agent/weave-issue-1")
	gitT(t, root, "merge", "-q", "--no-ff", "-m", "merge issue 1", "agent/weave-issue-1")

	if !weaveItemMerged(root, "main", it) {
		t.Fatalf("expected merged after the sandbox commit is an ancestor of main")
	}

	// No HEAD and no sandbox → conservatively not-merged.
	empty := &weaveItem{State: "submitted"}
	if weaveItemMerged(root, "main", empty) {
		t.Fatalf("expected not-merged with no recorded head and no sandbox")
	}

	// Zero commits ahead: HEAD equals the base commit, a trivial
	// ancestor of itself. This is an "empty" run (nothing to merge), NOT
	// a merged one — reconciling it to done would wrongly drop a clean
	// submitted item out of the list. CommitsAhead==0 must read as
	// not-merged even though the sha is technically reachable from base.
	baseSha := gitT(t, root, "rev-parse", "main")
	emptyRun := &weaveItem{State: "submitted", Head: baseSha, CommitsAhead: 0}
	if weaveItemMerged(root, "main", emptyRun) {
		t.Fatalf("expected not-merged for a zero-commit (empty) run")
	}
}

func TestWeaveItemMergedFallsBackToSandboxHead(t *testing.T) {
	root, sandbox, sha := setupMergeFixture(t)
	gitT(t, root, "fetch", "-q", sandbox, "agent/weave-issue-1:agent/weave-issue-1")
	gitT(t, root, "merge", "-q", "--no-ff", "-m", "merge issue 1", "agent/weave-issue-1")

	// Head unset on the item: weaveItemMerged should read the sandbox's
	// live HEAD as a fallback and still resolve the merge state.
	it := &weaveItem{State: "submitted", Sandbox: sandbox, CommitsAhead: 1}
	if !weaveItemMerged(root, "main", it) {
		t.Fatalf("expected merged via sandbox-HEAD fallback (sha %s)", sha[:7])
	}
}

func TestWeaveReconcileMerged(t *testing.T) {
	root, sandbox, sha := setupMergeFixture(t)
	gitT(t, root, "fetch", "-q", sandbox, "agent/weave-issue-1:agent/weave-issue-1")
	gitT(t, root, "merge", "-q", "--no-ff", "-m", "merge issue 1", "agent/weave-issue-1")

	q := &weaveQueue{Items: []*weaveItem{
		{ID: 1, State: "submitted", Head: sha, Sandbox: sandbox, CommitsAhead: 1}, // merged → flip
		{ID: 2, State: "submitted", Head: "deadbeef" + sha[8:], CommitsAhead: 1},  // bogus/unmerged → keep
		{ID: 3, State: "working", Head: sha, CommitsAhead: 1},                     // not submitted → keep
	}}
	n := weaveReconcileMerged(root, "main", q)
	if n != 1 {
		t.Fatalf("expected 1 reconciled, got %d", n)
	}
	if q.Items[0].State != "done" {
		t.Errorf("issue 1: expected done, got %q", q.Items[0].State)
	}
	if q.Items[1].State != "submitted" {
		t.Errorf("issue 2: expected submitted (unmerged), got %q", q.Items[1].State)
	}
	if q.Items[2].State != "working" {
		t.Errorf("issue 3: expected working (untouched), got %q", q.Items[2].State)
	}
}

func TestWeaveTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 40, "short"},
		{"exactly-ten", 11, "exactly-ten"},
		{strings.Repeat("a", 50), 40, strings.Repeat("a", 37) + "..."},
		// Multibyte: byte-slicing would split the emoji mid-rune and emit
		// the U+FFFD replacement char; rune-slicing must not.
		{"recurrence 🔁🔁🔁🔁 detail", 14, "recurrence " + "..."},
	}
	for _, c := range cases {
		got := weaveTruncate(c.in, c.max)
		if got != c.want {
			t.Errorf("weaveTruncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if strings.ContainsRune(got, '�') {
			t.Errorf("weaveTruncate(%q, %d) = %q contains a replacement char (mid-rune split)", c.in, c.max, got)
		}
	}
}
