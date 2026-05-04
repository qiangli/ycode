package service

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/runtime/usage"
)

// fakeApp implements AppBackend for testing.
type fakeApp struct {
	sessionID    string
	workDir      string
	model        string
	providerKind string
	messageCount int
	closed       bool
}

func (a *fakeApp) Session() *session.Session             { return &session.Session{ID: a.sessionID} }
func (a *fakeApp) SessionID() string                     { return a.sessionID }
func (a *fakeApp) SessionMessages() []api.Message        { return nil }
func (a *fakeApp) MessageCount() int                     { return a.messageCount }
func (a *fakeApp) Config() *config.Config                { return &config.Config{Model: a.model} }
func (a *fakeApp) Model() string                         { return a.model }
func (a *fakeApp) ProviderKind() string                  { return a.providerKind }
func (a *fakeApp) Version() string                       { return "test" }
func (a *fakeApp) WorkDir() string                       { return a.workDir }
func (a *fakeApp) InPlanMode() bool                      { return false }
func (a *fakeApp) SwitchModel(string) (string, error)    { return "", nil }
func (a *fakeApp) UsageTracker() *usage.Tracker          { return usage.NewTracker() }
func (a *fakeApp) NextTurnIndex() int                    { return 0 }
func (a *fakeApp) HasCommand(string) bool                { return false }
func (a *fakeApp) SetProgressFunc(func(string))          {}
func (a *fakeApp) SetDeltaFunc(func(string))             {}
func (a *fakeApp) SetUsageFunc(func(int, int, int, int)) {}
func (a *fakeApp) Close() error                          { a.closed = true; return nil }

func (a *fakeApp) ExecuteCommand(ctx context.Context, name, args string) (string, error) {
	return "", nil
}

func (a *fakeApp) ConversationRuntime() *conversation.Runtime { return nil }
func (a *fakeApp) RunTurnWithRecovery(ctx context.Context, messages []api.Message) (*conversation.TurnResult, *conversation.RecoveryResult, error) {
	return nil, nil, nil
}
func (a *fakeApp) ExecuteTools(ctx context.Context, calls []conversation.ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	return nil
}

func TestSessionPool_GetOrCreate(t *testing.T) {
	counter := 0
	factory := func(workDir string) (AppBackend, error) {
		counter++
		return &fakeApp{
			sessionID: "sid-" + workDir,
			workDir:   workDir,
			model:     "test-model",
		}, nil
	}

	pool := NewSessionPool(factory)

	// First call creates a new session.
	ms1, err := pool.GetOrCreate("/project-a")
	if err != nil {
		t.Fatal(err)
	}
	if ms1.ID != "sid-/project-a" {
		t.Errorf("expected sid-/project-a, got %s", ms1.ID)
	}
	if counter != 1 {
		t.Errorf("expected factory called once, got %d", counter)
	}

	// Second call for same workDir reuses.
	ms2, err := pool.GetOrCreate("/project-a")
	if err != nil {
		t.Fatal(err)
	}
	if ms2.ID != ms1.ID {
		t.Errorf("expected same session, got %s", ms2.ID)
	}
	if counter != 1 {
		t.Errorf("expected factory not called again, got %d", counter)
	}

	// Different workDir creates new session.
	ms3, err := pool.GetOrCreate("/project-b")
	if err != nil {
		t.Fatal(err)
	}
	if ms3.ID == ms1.ID {
		t.Error("expected different session for different workDir")
	}
	if counter != 2 {
		t.Errorf("expected factory called twice, got %d", counter)
	}

	if pool.Count() != 2 {
		t.Errorf("expected 2 sessions, got %d", pool.Count())
	}
}

func TestSessionPool_SeedSession(t *testing.T) {
	pool := NewSessionPool(nil)
	app := &fakeApp{sessionID: "primary", workDir: "/server-cwd"}
	pool.SeedSession(app)

	ms := pool.Get("primary")
	if ms == nil {
		t.Fatal("seeded session not found")
	}
	if ms.WorkDir != "/server-cwd" {
		t.Errorf("expected /server-cwd, got %s", ms.WorkDir)
	}

	ms2 := pool.GetByWorkDir("/server-cwd")
	if ms2 == nil {
		t.Fatal("seeded session not found by workDir")
	}
}

func TestSessionPool_Remove(t *testing.T) {
	app := &fakeApp{sessionID: "s1", workDir: "/p1"}
	pool := NewSessionPool(nil)
	pool.SeedSession(app)

	if err := pool.Remove("s1"); err != nil {
		t.Fatal(err)
	}
	if !app.closed {
		t.Error("expected app to be closed")
	}
	if pool.Count() != 0 {
		t.Error("expected empty pool")
	}
}

func TestSessionPool_List(t *testing.T) {
	pool := NewSessionPool(nil)
	pool.SeedSession(&fakeApp{sessionID: "s1", workDir: "/a"})
	pool.SeedSession(&fakeApp{sessionID: "s2", workDir: "/b"})

	infos := pool.List()
	if len(infos) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(infos))
	}
}

func TestSessionPool_Close(t *testing.T) {
	a1 := &fakeApp{sessionID: "s1", workDir: "/a"}
	a2 := &fakeApp{sessionID: "s2", workDir: "/b"}
	pool := NewSessionPool(nil)
	pool.SeedSession(a1)
	pool.SeedSession(a2)

	pool.Close()
	if !a1.closed || !a2.closed {
		t.Error("expected all apps closed")
	}
	if pool.Count() != 0 {
		t.Error("expected empty pool after close")
	}
}
