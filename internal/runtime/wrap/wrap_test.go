package wrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripShimFromPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	cases := []struct {
		name    string
		path    string
		shimDir string
		want    string
	}{
		{
			name:    "removes single entry",
			path:    "/tmp/ycode-wrap/123/bin" + sep + "/usr/bin" + sep + "/bin",
			shimDir: "/tmp/ycode-wrap/123/bin",
			want:    "/usr/bin" + sep + "/bin",
		},
		{
			name:    "removes only the shim entry, preserves others",
			path:    "/usr/local/bin" + sep + "/tmp/ycode-wrap/123/bin" + sep + "/usr/bin",
			shimDir: "/tmp/ycode-wrap/123/bin",
			want:    "/usr/local/bin" + sep + "/usr/bin",
		},
		{
			name:    "no shim entry leaves path untouched",
			path:    "/usr/local/bin" + sep + "/usr/bin",
			shimDir: "/tmp/ycode-wrap/123/bin",
			want:    "/usr/local/bin" + sep + "/usr/bin",
		},
		{
			name:    "empty shim dir leaves path untouched",
			path:    "/usr/local/bin",
			shimDir: "",
			want:    "/usr/local/bin",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripShimFromPath(tc.path, tc.shimDir)
			if got != tc.want {
				t.Fatalf("stripShimFromPath\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestInjectShimEnvOverridesPathAndShell(t *testing.T) {
	base := []string{"FOO=bar", "PATH=/usr/local/bin:/usr/bin", "SHELL=/bin/zsh"}
	opts := Options{AgentArgs: []string{"claude", "-p", "hi"}}
	out := injectShimEnv(base, "/tmp/ycode-wrap/abc/bin", opts)

	got := map[string]string{}
	for _, kv := range out {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		got[kv[:eq]] = kv[eq+1:]
	}

	if got["FOO"] != "bar" {
		t.Errorf("FOO leaked: got %q", got["FOO"])
	}
	if !strings.HasPrefix(got["PATH"], "/tmp/ycode-wrap/abc/bin"+string(os.PathListSeparator)) {
		t.Errorf("PATH does not start with shim dir: %q", got["PATH"])
	}
	if !strings.Contains(got["PATH"], "/usr/bin") {
		t.Errorf("original PATH entries lost: %q", got["PATH"])
	}
	if got["SHELL"] != filepath.Join("/tmp/ycode-wrap/abc/bin", "bash") {
		t.Errorf("SHELL not set to shim bash: %q", got["SHELL"])
	}
	if got[envShim] != "1" {
		t.Errorf("%s not set to 1: %q", envShim, got[envShim])
	}
	if got[envDepth] != "0" {
		t.Errorf("%s not set to 0: %q", envDepth, got[envDepth])
	}
	if got[envShimDir] != "/tmp/ycode-wrap/abc/bin" {
		t.Errorf("%s missing: %q", envShimDir, got[envShimDir])
	}
	if got[envWrappedAgent] != "claude" {
		t.Errorf("%s != claude: %q", envWrappedAgent, got[envWrappedAgent])
	}
}

func TestMaterializeShimDirCreatesSymlinks(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem test skipped under -short")
	}

	// Use a real path that exists for the symlink target — the
	// running test binary itself.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	// Override XDG_RUNTIME_DIR so the shim root lands under TempDir
	// and gets reaped with the test, and so we don't pollute the
	// user's runtime dir if running locally.
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	dir, sessionDir, err := materializeShimDir(self, []string{"bash", "rg", "ycode"}) // "ycode" must be skipped
	if err != nil {
		t.Fatalf("materializeShimDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sessionDir) })

	for _, name := range []string{"bash", "rg"} {
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("expected shim %s: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 && info.Mode()&0o111 == 0 {
			t.Errorf("shim %s is neither symlink nor executable copy: mode=%v", name, info.Mode())
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "ycode")); !os.IsNotExist(err) {
		t.Errorf("ycode shim should have been skipped; Lstat err=%v", err)
	}
}

func TestIsShimInvocationGate(t *testing.T) {
	// Without the env var, never a shim regardless of argv[0].
	t.Setenv(envShim, "")
	if IsShimInvocation() {
		t.Errorf("IsShimInvocation true without %s env", envShim)
	}
	// With the env var but argv[0]==ycode, still not a shim.
	t.Setenv(envShim, "1")
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = []string{"/usr/local/bin/ycode"}
	if IsShimInvocation() {
		t.Errorf("IsShimInvocation true for argv[0]=ycode")
	}
	os.Args = []string{"/tmp/ycode-wrap/123/bin/bash"}
	if !IsShimInvocation() {
		t.Errorf("IsShimInvocation false for shim invocation")
	}
}
