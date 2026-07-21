package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/datadir"
	"github.com/qiangli/ycode/pkg/memex/store"
	"github.com/qiangli/ycode/pkg/memex/store/kv"
)

// openStore mirrors the storage wiring in newAppFromFlags: resolve the root,
// open bbolt with a bounded wait for the single-writer lock, and only tolerate
// failure when a degraded run was explicitly requested.
func openStore(t *testing.T, dir string, degraded bool, wait time.Duration) (*store.Manager, error) {
	t.Helper()
	return store.NewManager(context.Background(), store.Config{
		DataDir:       dir,
		AllowDegraded: degraded,
		KVFactory: func(context.Context) (store.KVStore, error) {
			return kv.OpenWithTimeout(dir, wait)
		},
	})
}

// TestStoreIsolation_PerSessionDataDir is the isolation half of the gate: two
// sessions pointed at distinct YCODE_DATA_DIR roots BOTH get a working store,
// concurrently. Before per-session roots, the second one lost the host-global
// lock and ran blind.
func TestStoreIsolation_PerSessionDataDir(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "session-a")
	dirB := filepath.Join(root, "session-b")

	t.Setenv(datadir.EnvDataDir, dirA)
	if got := datadir.Resolve("/unused-home"); got != dirA {
		t.Fatalf("Resolve with %s = %q, want %q", datadir.EnvDataDir, got, dirA)
	}

	mgrA, err := openStore(t, dirA, false, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("session A store: %v", err)
	}
	defer mgrA.Close()

	mgrB, err := openStore(t, dirB, false, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("session B store (concurrent, distinct data dir): %v", err)
	}
	defer mgrB.Close()

	// Both must actually WRITE — "opened" is not the same as usable.
	for name, mgr := range map[string]*store.Manager{"A": mgrA, "B": mgrB} {
		kvs := mgr.KV()
		if kvs == nil {
			t.Fatalf("session %s: KV store is nil", name)
		}
		if err := kvs.Put("test", "k", []byte(name)); err != nil {
			t.Fatalf("session %s: put: %v", name, err)
		}
		got, err := kvs.Get("test", "k")
		if err != nil || string(got) != name {
			t.Fatalf("session %s: get = %q, %v; want %q", name, got, err, name)
		}
	}
}

// TestStoreContention_FailsLoud is the fail-loud half of the gate: a second
// session on the SAME data dir must not silently degrade. It waits, then
// errors — and the error must name the escape hatches, because the whole point
// is that an operator reading it knows what to do.
func TestStoreContention_FailsLoud(t *testing.T) {
	dir := t.TempDir()

	holder, err := openStore(t, dir, false, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first session store: %v", err)
	}
	defer holder.Close()

	start := time.Now()
	wait := 300 * time.Millisecond
	if _, err := openStore(t, dir, false, wait); err == nil {
		t.Fatal("second session on a contended data dir succeeded; want a hard failure")
	} else {
		if !errors.Is(err, store.ErrUnavailable) {
			t.Errorf("error = %v, want it to wrap store.ErrUnavailable", err)
		}
		if !errors.Is(err, store.ErrLocked) {
			t.Errorf("error = %v, want it to wrap store.ErrLocked", err)
		}
		msg := storeInitError(dir, wait, err).Error()
		for _, want := range []string{dir, datadir.EnvDataDir, datadir.EnvNoStore, "--no-store"} {
			if !strings.Contains(msg, want) {
				t.Errorf("operator message missing %q:\n%s", want, msg)
			}
		}
	}
	// It must WAIT for the lock, not fail instantly — a peer shutting down
	// should be ridden out rather than reported. bbolt polls the lock on a
	// coarse tick, so allow one tick of slack against the budget.
	const tick = 60 * time.Millisecond
	if elapsed := time.Since(start); elapsed < wait-tick {
		t.Errorf("gave up after %s, want roughly the %s wait budget", elapsed, wait)
	}
}

// TestStoreContention_DegradedIsOptIn: --no-store / YCODE_NO_STORE=1 is the
// only way a contended session continues, and it continues visibly store-less.
func TestStoreContention_DegradedIsOptIn(t *testing.T) {
	dir := t.TempDir()

	holder, err := openStore(t, dir, false, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first session store: %v", err)
	}
	defer holder.Close()

	mgr, err := openStore(t, dir, true, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("explicit degraded run should start, got: %v", err)
	}
	defer mgr.Close()
	if mgr.KV() != nil {
		t.Error("degraded run reports a KV store; want nil so callers see the truth")
	}

	t.Setenv(datadir.EnvNoStore, "1")
	if !datadir.NoStore() {
		t.Errorf("%s=1 not honored", datadir.EnvNoStore)
	}
}

func TestDataDirResolution(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		home string
		want string
	}{
		{
			name: "default is the host-global root",
			home: "/home/u",
			want: filepath.Join("/home/u", ".agents", "ycode", "projects", "data"),
		},
		{
			name: "YCODE_HOME relocates the whole tree",
			env:  map[string]string{datadir.EnvHome: "/tmp/agents"},
			home: "/home/u",
			want: filepath.Join("/tmp/agents", "projects", "data"),
		},
		{
			name: "YCODE_DATA_DIR wins outright",
			env:  map[string]string{datadir.EnvHome: "/tmp/agents", datadir.EnvDataDir: "/tmp/session-7"},
			home: "/home/u",
			want: "/tmp/session-7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := datadir.ResolveWith(tt.home, func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Errorf("Resolve = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLockTimeoutEnv(t *testing.T) {
	t.Setenv(datadir.EnvLockTimeout, "2s")
	if got := datadir.LockTimeout(); got != 2*time.Second {
		t.Errorf("LockTimeout = %s, want 2s", got)
	}
	t.Setenv(datadir.EnvLockTimeout, "not-a-duration")
	if got := datadir.LockTimeout(); got != datadir.DefaultLockTimeout {
		t.Errorf("LockTimeout on garbage = %s, want the %s default", got, datadir.DefaultLockTimeout)
	}
}
