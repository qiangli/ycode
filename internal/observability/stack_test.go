package observability

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// mockComponent implements Component for testing.
type mockComponent struct {
	name     string
	started  atomic.Bool
	healthy  atomic.Bool
	startErr error
	stopErr  error
}

func (m *mockComponent) Name() string { return m.name }

func (m *mockComponent) Start(_ context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started.Store(true)
	m.healthy.Store(true)
	return nil
}

func (m *mockComponent) Stop(_ context.Context) error {
	m.started.Store(false)
	m.healthy.Store(false)
	return m.stopErr
}

func (m *mockComponent) Healthy() bool             { return m.healthy.Load() }
func (m *mockComponent) HTTPHandler() http.Handler { return nil }

func TestStackManager_StartStop(t *testing.T) {
	cfg := &config.ObservabilityConfig{
		ProxyPort:     freePort(t),
		ProxyBindAddr: "127.0.0.1",
	}
	sm := NewStackManager(cfg, t.TempDir())

	c1 := &mockComponent{name: "comp-a"}
	c2 := &mockComponent{name: "comp-b"}
	sm.AddComponent(c1)
	sm.AddComponent(c2)

	ctx := context.Background()
	if err := sm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !c1.healthy.Load() {
		t.Error("comp-a should be healthy after start")
	}
	if !c2.healthy.Load() {
		t.Error("comp-b should be healthy after start")
	}
	if !sm.Healthy() {
		t.Error("stack should be healthy")
	}

	statuses := sm.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	for _, s := range statuses {
		if !s.Healthy {
			t.Errorf("component %s should be healthy", s.Name)
		}
	}

	if err := sm.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if c1.healthy.Load() {
		t.Error("comp-a should be unhealthy after stop")
	}
	if c2.healthy.Load() {
		t.Error("comp-b should be unhealthy after stop")
	}
	if sm.Healthy() {
		t.Error("stack should not be healthy after stop")
	}
}

func TestStackManager_DoubleStart(t *testing.T) {
	cfg := &config.ObservabilityConfig{
		ProxyPort:     freePort(t),
		ProxyBindAddr: "127.0.0.1",
	}
	sm := NewStackManager(cfg, t.TempDir())
	ctx := context.Background()

	if err := sm.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer sm.Stop(ctx)

	err := sm.Start(ctx)
	if err == nil {
		t.Error("second Start should return error")
	}
}

func TestStackManager_ComponentStartFailure(t *testing.T) {
	cfg := &config.ObservabilityConfig{
		ProxyPort:     freePort(t),
		ProxyBindAddr: "127.0.0.1",
	}
	sm := NewStackManager(cfg, t.TempDir())

	good := &mockComponent{name: "good"}
	bad := &mockComponent{name: "bad", startErr: context.DeadlineExceeded}
	sm.AddComponent(bad)
	sm.AddComponent(good)

	ctx := context.Background()
	// Start should succeed despite one component failing (non-fatal).
	if err := sm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sm.Stop(ctx)

	// Good component should still be running.
	if !good.healthy.Load() {
		t.Error("good component should be healthy")
	}
	// Bad component should not be running.
	if bad.healthy.Load() {
		t.Error("bad component should not be healthy")
	}
}

func TestStackManager_StopWithoutStart(t *testing.T) {
	cfg := &config.ObservabilityConfig{}
	sm := NewStackManager(cfg, t.TempDir())

	// Stop without start should be no-op.
	if err := sm.Stop(context.Background()); err != nil {
		t.Fatalf("Stop without start should not error: %v", err)
	}
}
