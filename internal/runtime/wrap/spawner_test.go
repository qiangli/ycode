//go:build !windows

package wrap

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/qiangli/ycode/internal/runtime/spawncore"
	"github.com/qiangli/ycode/internal/runtime/wrap/spawn_embed"
)

// TestSpawnerShimDispatch drives the embedded ycode-spawn micro shim
// end to end: materialize a shim dir, invoke a tool through a shim
// symlink, and assert (a) the real tool ran with the right exit code
// and output, (b) the spawn-event datagram reached the session
// listener, (c) the depth ceiling refuses with 125.
//
// Skips when the embed is absent (bare `go test` without
// -tags embed_spawn); `make test` carries the tag once
// scripts/embed-spawn.sh has produced the .gz.
func TestSpawnerShimDispatch(t *testing.T) {
	if !spawn_embed.Available() {
		t.Skip("no embedded ycode-spawn (run scripts/embed-spawn.sh and test with -tags embed_spawn)")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, sessionDir, err := materializeShimDir(self, []string{"echo", "git"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sessionDir)

	// Shims must point at the extracted micro shim, not at self.
	spawner := filepath.Join(dir, ".ycode-spawn")
	if _, err := os.Stat(spawner); err != nil {
		t.Fatalf("spawner not extracted: %v", err)
	}
	link, err := os.Readlink(filepath.Join(dir, "git"))
	if err != nil || link != spawner {
		t.Fatalf("git shim points at %q (err=%v), want %q", link, err, spawner)
	}

	listener, err := startSpawnEventListener(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.stop()

	env := append(os.Environ(),
		spawncore.EnvShim+"=1",
		spawncore.EnvShimDir+"="+dir,
		spawncore.EnvDepth+"=0",
		spawncore.EnvEvents+"="+listener.sockPath,
	)

	// (a) dispatch through the shim: real echo must run.
	cmd := exec.Command(filepath.Join(dir, "echo"), "hello-from-spawner")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shim echo failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "hello-from-spawner" {
		t.Fatalf("shim echo output = %q", got)
	}

	// (b) the spawn event landed in the aggregate.
	deadline := time.Now().Add(3 * time.Second)
	for {
		total, top := listener.stats.snapshot(5)
		if total >= 1 && strings.Contains(top, "echo=") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("spawn event never aggregated (total=%d top=%q)", total, top)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// (c) depth ceiling refuses with 125.
	cmd = exec.Command(filepath.Join(dir, "echo"), "nope")
	cmd.Env = append(os.Environ(),
		spawncore.EnvShim+"=1",
		spawncore.EnvShimDir+"="+dir,
		spawncore.EnvDepth+"=4",
	)
	err = cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 125 {
		t.Fatalf("depth-ceiling dispatch err = %v, want exit 125", err)
	}
}

// TestSpawnListener_AggregatesAndStops exercises the listener without
// the spawner binary, so it runs even without the embed tag.
func TestSpawnListener_AggregatesAndStops(t *testing.T) {
	// Not t.TempDir(): unix socket paths cap at ~104 bytes on macOS
	// and test tempdir names blow past it.
	sessionDir, err := os.MkdirTemp("/tmp", "wrapev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sessionDir)
	listener, err := startSpawnEventListener(sessionDir)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("unixgram", listener.sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, payload := range []string{
		`{"ev":"spawn","tool":"git","pid":1,"ppid":0,"depth":0}`,
		`{"ev":"spawn","tool":"git","pid":2,"ppid":0,"depth":0}`,
		`{"ev":"spawn","tool":"bash","pid":3,"ppid":0,"depth":1}`,
		`not-json`, // must be ignored, not crash the loop
	} {
		if _, err := conn.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		total, top := listener.stats.snapshot(5)
		if total == 3 {
			if !strings.HasPrefix(top, "git=2") {
				t.Fatalf("top line = %q, want git=2 first", top)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("aggregation incomplete: total=%d", total)
		}
		time.Sleep(20 * time.Millisecond)
	}

	listener.stop()
	listener.stop() // idempotent
}

// TestSpawnListener_ExitEventRecordsSpan: an "exit" datagram from a
// span-mode shim becomes a real, back-dated OTel span carrying tool /
// exit-code / duration attributes.
func TestSpawnListener_ExitEventRecordsSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	sessionDir, err := os.MkdirTemp("/tmp", "wrapev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sessionDir)
	listener, err := startSpawnEventListener(sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.stop()
	listener.enableSpans(context.Background())

	conn, err := net.Dial("unixgram", listener.sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"ev":"exit","tool":"git","pid":42,"ppid":1,"depth":1,"exit_code":3,"dur_ms":250}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		spans := exporter.GetSpans()
		if len(spans) == 1 {
			sp := spans[0]
			if sp.Name != "ycode.exec.spawned_tool" {
				t.Fatalf("span name = %q", sp.Name)
			}
			attrs := map[string]any{}
			for _, kv := range sp.Attributes {
				attrs[string(kv.Key)] = kv.Value.AsInterface()
			}
			if attrs["exec.tool"] != "git" || attrs["exec.exit_code"] != int64(3) || attrs["exec.duration_ms"] != int64(250) {
				t.Fatalf("span attrs = %v", attrs)
			}
			if d := sp.EndTime.Sub(sp.StartTime); d < 200*time.Millisecond || d > time.Second {
				t.Fatalf("span duration = %v, want ~250ms (back-dated start)", d)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("span never exported (have %d)", len(exporter.GetSpans()))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
