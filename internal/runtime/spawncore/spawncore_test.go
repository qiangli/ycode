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

func TestEmitSpawn_Datagram(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ev.sock")
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
