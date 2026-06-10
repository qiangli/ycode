//go:build !windows

package spawncore

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStripPathEntry(t *testing.T) {
	sep := string(os.PathListSeparator)
	cases := []struct {
		path, dir, want string
	}{
		{"/a" + sep + "/shim" + sep + "/b", "/shim", "/a" + sep + "/b"},
		{"/shim" + sep + "/shim" + sep + "/b", "/shim", "/b"},
		{"/a" + sep + "/b", "/shim", "/a" + sep + "/b"},
		{"/a" + sep + "/b", "", "/a" + sep + "/b"},
		{"/shim", "/shim", ""},
	}
	for _, c := range cases {
		if got := StripPathEntry(c.path, c.dir); got != c.want {
			t.Errorf("StripPathEntry(%q, %q) = %q, want %q", c.path, c.dir, got, c.want)
		}
	}
}

func TestDispatch_DepthGuard(t *testing.T) {
	t.Setenv(EnvDepth, "4")
	if got := Dispatch("bash", nil); got != 125 {
		t.Fatalf("Dispatch past depth ceiling = %d, want 125", got)
	}
}

func TestDispatch_RealNotFound(t *testing.T) {
	t.Setenv(EnvDepth, "0")
	t.Setenv(EnvShimDir, "")
	t.Setenv("PATH", t.TempDir()) // empty dir: nothing resolvable
	if got := Dispatch("definitely-not-a-real-tool-xyz", nil); got != 127 {
		t.Fatalf("Dispatch with unresolvable tool = %d, want 127", got)
	}
}

// shortTempSock returns a socket path under /tmp short enough for the
// ~104-byte unix sun_path limit (t.TempDir() names blow past it).
func shortTempSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "ev.sock")
}

func TestEmitSpawn_Datagram(t *testing.T) {
	sock := shortTempSock(t)
	addr, err := net.ResolveUnixAddr("unixgram", sock)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	t.Setenv(EnvEvents, sock)
	EmitSpawn("git", 2)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("no datagram received: %v", err)
	}
	var ev SpawnEvent
	if err := json.Unmarshal(buf[:n], &ev); err != nil {
		t.Fatalf("bad payload: %v (%s)", err, buf[:n])
	}
	if ev.Ev != "spawn" || ev.Tool != "git" || ev.Depth != 2 || ev.PID != os.Getpid() {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestEmitSpawn_NoSocketIsNoop(t *testing.T) {
	t.Setenv(EnvEvents, "")
	EmitSpawn("git", 0) // must not panic or block
	t.Setenv(EnvEvents, "/nonexistent/path/ev.sock")
	EmitSpawn("git", 0) // dial failure ignored
}

func TestDispatch_SpanModeWaitsAndEmitsExit(t *testing.T) {
	sock := shortTempSock(t)
	addr, err := net.ResolveUnixAddr("unixgram", sock)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	t.Setenv(EnvDepth, "0")
	t.Setenv(EnvShimDir, "")
	t.Setenv(EnvEvents, sock)
	t.Setenv(EnvSpawnTrace, "1")
	t.Setenv("PATH", "/bin:/usr/bin")

	// In span mode Dispatch fork-and-waits and RETURNS (no exec), so
	// it is testable in-process. exit 7 proves code propagation.
	if got := Dispatch("sh", []string{"-c", "exit 7"}); got != 7 {
		t.Fatalf("span-mode Dispatch exit = %d, want 7", got)
	}

	// Two datagrams: spawn, then exit with code+duration.
	var sawSpawn, sawExit bool
	buf := make([]byte, 4096)
	for i := 0; i < 2; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := conn.ReadFromUnix(buf)
		if err != nil {
			t.Fatalf("datagram %d missing: %v", i, err)
		}
		var ev SpawnEvent
		if err := json.Unmarshal(buf[:n], &ev); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		switch ev.Ev {
		case "spawn":
			sawSpawn = true
		case "exit":
			sawExit = true
			if ev.Tool != "sh" || ev.ExitCode == nil || *ev.ExitCode != 7 {
				t.Fatalf("exit event = %+v, want tool=sh exit_code=7", ev)
			}
		}
	}
	if !sawSpawn || !sawExit {
		t.Fatalf("expected spawn+exit events (spawn=%v exit=%v)", sawSpawn, sawExit)
	}
}
