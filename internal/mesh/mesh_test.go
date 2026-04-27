package mesh

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
)

// fakeAgent is a minimal MeshAgent for testing.
type fakeAgent struct {
	name     string
	started  atomic.Bool
	stopped  atomic.Bool
	startErr error
}

func (f *fakeAgent) Name() string { return f.name }

func (f *fakeAgent) Start(_ context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started.Store(true)
	return nil
}

func (f *fakeAgent) Stop() {
	f.stopped.Store(true)
	f.started.Store(false)
}

func (f *fakeAgent) Healthy() bool { return f.started.Load() }

func TestMeshRegister(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	m := New(nil, b)
	a1 := &fakeAgent{name: "alpha"}
	a2 := &fakeAgent{name: "beta"}
	m.Register(a1)
	m.Register(a2)

	status := m.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(status))
	}
	if status["alpha"] || status["beta"] {
		t.Fatal("agents should not be healthy before Start")
	}
}

func TestMeshStartStop(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	m := New(nil, b)
	a := &fakeAgent{name: "worker"}
	m.Register(a)

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	status := m.Status()
	if !status["worker"] {
		t.Fatal("agent should be healthy after Start")
	}

	// Double start is a no-op.
	if err := m.Start(ctx); err != nil {
		t.Fatalf("second Start should be no-op: %v", err)
	}

	m.Stop()
	status = m.Status()
	if status["worker"] {
		t.Fatal("agent should not be healthy after Stop")
	}
}

func TestMeshStartAgentError(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	m := New(nil, b)
	good := &fakeAgent{name: "good"}
	bad := &fakeAgent{name: "bad", startErr: errors.New("boom")}
	m.Register(bad)
	m.Register(good)

	ctx := context.Background()
	// Start should not return error even if individual agents fail.
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start should succeed even with failing agent: %v", err)
	}

	status := m.Status()
	if status["bad"] {
		t.Fatal("bad agent should not be healthy")
	}
	if !status["good"] {
		t.Fatal("good agent should be healthy")
	}
}

func TestMeshStatus(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	m := New(nil, b)
	status := m.Status()
	if len(status) != 0 {
		t.Fatalf("empty mesh should have empty status, got %d entries", len(status))
	}
}

func TestDefaultMeshConfig(t *testing.T) {
	cfg := DefaultMeshConfig()
	if cfg.Enabled {
		t.Fatal("default config should not be enabled")
	}
	if cfg.Mode != "cli" {
		t.Fatalf("expected mode cli, got %s", cfg.Mode)
	}
	if cfg.MaxFixAttempts != 5 {
		t.Fatalf("expected MaxFixAttempts 5, got %d", cfg.MaxFixAttempts)
	}
}
