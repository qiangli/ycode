// Hermetic regression for the `dag ci` mount plan (scripts/ci-run.sh).
//
// The bug this pins down: with the old `-v "$PWD":/src` invocation, a checkout
// whose git-dir lives outside the worktree — the dhnt umbrella submodule
// topology, where .git is a file pointing at ../.git/modules/ycode — mounts a
// worktree whose .git pointer dangles inside the container, and every
// git-derived gate step dies with "fatal: not a git repository". The fix is to
// mount the worktree, its sibling modules, and (when it lives elsewhere) the
// resolved git-common-dir at their real host paths so both the git pointer and
// go.mod's ../<sibling> replacements resolve identically inside and outside
// the container.
//
// These tests rebuild both topologies with real git in temp dirs and assert on
// `ci-run.sh --print-mounts`, so they need git and bash but no container
// engine.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"git", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

// hermeticGit runs git isolated from the developer's global/system config
// (hooks paths, protocol policies, etc. must not leak into the fixture).
func hermeticGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=ci-mounts-test",
		"GIT_AUTHOR_EMAIL=ci-mounts-test@invalid",
		"GIT_COMMITTER_NAME=ci-mounts-test",
		"GIT_COMMITTER_EMAIL=ci-mounts-test@invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// printMounts runs `ci-run.sh --print-mounts` with dir as the working
// directory and returns the emitted "src:dst" specs.
func printMounts(t *testing.T, dir string) []string {
	t.Helper()
	script, err := filepath.Abs("ci-run.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "--print-mounts")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ci-run.sh --print-mounts: %v\n%s", err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// canonical resolves symlinks the way git's absolute path-format does, so
// expectations survive macOS's /var → /private/var indirection in TempDir.
func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

var ciSiblings = []string{"sh", "nadir", "coreutils"}

func createSiblingFixture(t *testing.T, parent string) []string {
	t.Helper()
	pins := make([]string, 0, len(ciSiblings))
	paths := make([]string, 0, len(ciSiblings))
	for _, name := range ciSiblings {
		path := filepath.Join(parent, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		pins = append(pins, name+"=fixture-sha")
		paths = append(paths, path+":"+path)
	}
	return append([]string{strings.Join(pins, "\n") + "\n"}, paths...)
}

// Standalone clone: the git dir is worktree/.git, inside the tree, so the plan
// needs no separate git mount. Its three sibling module mounts are still
// required for go.mod's local replacements.
func TestCIMountsStandaloneClone(t *testing.T) {
	requireTools(t)
	repo := canonical(t, t.TempDir())
	hermeticGit(t, repo, "init", "-q")
	fixture := createSiblingFixture(t, filepath.Dir(repo))
	if err := os.WriteFile(filepath.Join(repo, ".sibling-pins"), []byte(fixture[0]), 0o644); err != nil {
		t.Fatal(err)
	}

	mounts := printMounts(t, repo)
	want := append([]string{repo + ":" + repo}, fixture[1:]...)
	if strings.Join(mounts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("standalone clone: got mounts %q, want %q", mounts, want)
	}
}

// Umbrella submodule: .git is a file pointing at the umbrella's
// .git/modules/<name>, outside the worktree. The plan must self-map both the
// worktree and the resolved git-common-dir so the relative pointer resolves
// inside the container.
func TestCIMountsUmbrellaSubmodule(t *testing.T) {
	requireTools(t)
	tmp := canonical(t, t.TempDir())

	origin := filepath.Join(tmp, "origin")
	if err := os.Mkdir(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	hermeticGit(t, origin, "init", "-q")
	hermeticGit(t, origin, "commit", "-q", "--allow-empty", "-m", "seed")

	umbrella := filepath.Join(tmp, "umbrella")
	if err := os.Mkdir(umbrella, 0o755); err != nil {
		t.Fatal(err)
	}
	hermeticGit(t, umbrella, "init", "-q")
	hermeticGit(t, umbrella, "-c", "protocol.file.allow=always",
		"submodule", "add", "-q", origin, "ycode")

	worktree := filepath.Join(umbrella, "ycode")
	gitCommonDir := filepath.Join(umbrella, ".git", "modules", "ycode")
	fixture := createSiblingFixture(t, umbrella)
	if err := os.WriteFile(filepath.Join(worktree, ".sibling-pins"), []byte(fixture[0]), 0o644); err != nil {
		t.Fatal(err)
	}

	// Guard the fixture itself: this test is only meaningful if git really
	// produced the file-pointer topology the bug depends on.
	if info, err := os.Lstat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
		t.Fatalf("fixture: expected submodule .git to be a gitdir pointer file, got err=%v isDir=%v", err, info != nil && info.IsDir())
	}
	if _, err := os.Stat(gitCommonDir); err != nil {
		t.Fatalf("fixture: expected submodule git dir at %s: %v", gitCommonDir, err)
	}

	mounts := printMounts(t, worktree)
	want := []string{
		worktree + ":" + worktree,
		gitCommonDir + ":" + gitCommonDir,
		// go's VCS root walks up past the .git file to the umbrella's .git
		// directory and stamps THAT repo — it must be mounted, or the gate
		// dies in-container on "error obtaining VCS status".
		umbrella + "/.git:" + umbrella + "/.git",
	}
	want = append(want, fixture[1:]...)
	if strings.Join(mounts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("umbrella submodule: got mounts %q, want %q", mounts, want)
	}
}

// Sibling submodules: inside the umbrella the siblings have the same
// file-pointer topology as ycode, and their gitdirs live in the UMBRELLA's
// .git/modules/<name> — not under any mounted worktree. The plan must mount
// each sibling's external git-common-dir too, or git-derived steps touching a
// sibling (go's VCS stamping across the go.work workspace) fail inside the
// container with "not a git repository" while passing outside it.
func TestCIMountsSiblingSubmoduleGitdirs(t *testing.T) {
	requireTools(t)
	tmp := canonical(t, t.TempDir())

	origin := filepath.Join(tmp, "origin")
	if err := os.Mkdir(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	hermeticGit(t, origin, "init", "-q")
	hermeticGit(t, origin, "commit", "-q", "--allow-empty", "-m", "seed")

	umbrella := filepath.Join(tmp, "umbrella")
	if err := os.Mkdir(umbrella, 0o755); err != nil {
		t.Fatal(err)
	}
	hermeticGit(t, umbrella, "init", "-q")
	hermeticGit(t, umbrella, "-c", "protocol.file.allow=always",
		"submodule", "add", "-q", origin, "ycode")
	// Make the FIRST pinned sibling (sh) a real submodule; the rest stay
	// plain directories, exercising both arms of the sibling-gitdir logic.
	hermeticGit(t, umbrella, "-c", "protocol.file.allow=always",
		"submodule", "add", "-q", origin, "sh")

	worktree := filepath.Join(umbrella, "ycode")
	ycodeCommon := filepath.Join(umbrella, ".git", "modules", "ycode")
	shCommon := filepath.Join(umbrella, ".git", "modules", "sh")
	fixture := createSiblingFixture(t, umbrella)
	if err := os.WriteFile(filepath.Join(worktree, ".sibling-pins"), []byte(fixture[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(umbrella, "sh", ".git")); err != nil || info.IsDir() {
		t.Fatalf("fixture: expected sibling .git to be a gitdir pointer file, got err=%v isDir=%v", err, info != nil && info.IsDir())
	}

	mounts := printMounts(t, worktree)
	shDir := filepath.Join(umbrella, "sh")
	want := []string{
		worktree + ":" + worktree,
		ycodeCommon + ":" + ycodeCommon,
		umbrella + "/.git:" + umbrella + "/.git", // go's VCS root for the submodule
		shDir + ":" + shDir,
		shCommon + ":" + shCommon,
	}
	// nadir and coreutils remain plain dirs: worktree mount only, no gitdir.
	want = append(want,
		filepath.Join(umbrella, "nadir")+":"+filepath.Join(umbrella, "nadir"),
		filepath.Join(umbrella, "coreutils")+":"+filepath.Join(umbrella, "coreutils"))
	if strings.Join(mounts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sibling submodules: got mounts %q, want %q", mounts, want)
	}
}

func TestCIRunRestoresGoWorkSum(t *testing.T) {
	requireTools(t)
	repo := canonical(t, t.TempDir())
	hermeticGit(t, repo, "init", "-q")
	fixture := createSiblingFixture(t, filepath.Dir(repo))
	if err := os.WriteFile(filepath.Join(repo, ".sibling-pins"), []byte(fixture[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	const original = "original workspace sums\n"
	if err := os.WriteFile(filepath.Join(repo, "go.work.sum"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeDocker := filepath.Join(t.TempDir(), "fake-docker")
	fake := `#!/bin/sh
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-w" ]; then
		shift
		worktree=$1
		break
	fi
	shift
done
printf 'container rewrite\n' > "$worktree/go.work.sum"
exit "${FAKE_DOCKER_EXIT:-0}"
`
	if err := os.WriteFile(fakeDocker, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	script, err := filepath.Abs("ci-run.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, exitCode := range []string{"0", "7"} {
		cmd := exec.Command("bash", script)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "DOCKER="+fakeDocker, "FAKE_DOCKER_EXIT="+exitCode)
		err = cmd.Run()
		if exitCode == "0" && err != nil {
			t.Fatalf("success fake docker: %v", err)
		}
		if exitCode != "0" && err == nil {
			t.Fatal("failing fake docker unexpectedly succeeded")
		}
		got, readErr := os.ReadFile(filepath.Join(repo, "go.work.sum"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != original {
			t.Fatalf("exit %s: go.work.sum = %q, want %q", exitCode, got, original)
		}
	}
}
