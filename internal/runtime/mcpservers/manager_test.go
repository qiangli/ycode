//go:build experimental

package mcpservers

import (
	"context"
	"errors"
	"testing"
)

// fakeService is a minimal Service used to drive Manager tests without
// spawning real backends.
type fakeService struct {
	name      string
	available bool
	readyErr  error
	executeFn func(action BrowserAction) (*BrowserResult, error)
	stopErr   error

	readyCalls int
	stopCalls  int
}

func (f *fakeService) Name() string                      { return f.name }
func (f *fakeService) Available(ctx context.Context) bool { return f.available }
func (f *fakeService) EnsureReady(ctx context.Context) error {
	f.readyCalls++
	return f.readyErr
}
func (f *fakeService) Stop(ctx context.Context) error {
	f.stopCalls++
	return f.stopErr
}
func (f *fakeService) Execute(ctx context.Context, action BrowserAction) (*BrowserResult, error) {
	if f.executeFn != nil {
		return f.executeFn(action)
	}
	return &BrowserResult{Success: true, URL: action.URL}, nil
}

func TestManager_RegisterAndDefault(t *testing.T) {
	m := NewManager()
	pw := &fakeService{name: ModeLive, available: true}
	m.Register(pw)

	if got := m.Default(); got != "" {
		t.Fatalf("default before SetDefault should be empty; got %q", got)
	}
	if err := m.SetDefault(ModeLive); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if got := m.Default(); got != ModeLive {
		t.Fatalf("default = %q; want %q", got, ModeLive)
	}
}

func TestManager_SetDefaultUnknown(t *testing.T) {
	m := NewManager()
	if err := m.SetDefault("nope"); err == nil {
		t.Fatal("SetDefault on unknown provider should error")
	}
}

func TestManager_ExecuteRoutesToDefault(t *testing.T) {
	m := NewManager()
	pw := &fakeService{name: ModeLive, available: true}
	dt := &fakeService{name: ModeProbe, available: true}
	m.Register(pw)
	m.Register(dt)

	if err := m.SetDefault(ModeProbe); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	dt.executeFn = func(action BrowserAction) (*BrowserResult, error) {
		return &BrowserResult{Success: true, Title: "from-probe", URL: action.URL}, nil
	}
	pw.executeFn = func(action BrowserAction) (*BrowserResult, error) {
		t.Fatal("live should not be called when default=probe")
		return nil, nil
	}

	res, err := m.Execute(context.Background(), BrowserAction{Type: "navigate", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Title != "from-probe" {
		t.Fatalf("routed to wrong backend; got title %q", res.Title)
	}
	if dt.readyCalls != 1 {
		t.Fatalf("EnsureReady should be called once; got %d", dt.readyCalls)
	}
}

func TestManager_ExecuteNoDefault(t *testing.T) {
	m := NewManager()
	m.Register(&fakeService{name: ModeLive, available: true})
	if _, err := m.Execute(context.Background(), BrowserAction{Type: "navigate"}); err == nil {
		t.Fatal("Execute without SetDefault should error")
	}
}

func TestManager_ExecuteReadyError(t *testing.T) {
	m := NewManager()
	bad := &fakeService{name: ModeLive, available: true, readyErr: errors.New("boom")}
	m.Register(bad)
	_ = m.SetDefault(ModeLive)
	if _, err := m.Execute(context.Background(), BrowserAction{Type: "navigate"}); err == nil {
		t.Fatal("Execute should propagate EnsureReady error")
	}
}

func TestManager_StopAll(t *testing.T) {
	m := NewManager()
	a := &fakeService{name: ModeLive, available: true}
	b := &fakeService{name: ModeProbe, available: true}
	m.Register(a)
	m.Register(b)
	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if a.stopCalls != 1 || b.stopCalls != 1 {
		t.Fatalf("StopAll should call each Service.Stop once; got a=%d b=%d", a.stopCalls, b.stopCalls)
	}
}

func TestManager_RegisterReplaces(t *testing.T) {
	m := NewManager()
	first := &fakeService{name: ModeLive, available: true}
	second := &fakeService{name: ModeLive, available: true}
	m.Register(first)
	m.Register(second)

	got, ok := m.Get(ModeLive)
	if !ok {
		t.Fatal("Get returned !ok")
	}
	if got != second {
		t.Fatal("Register should replace the previous instance")
	}
}
