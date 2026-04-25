package agentpool

import (
	"sync"
	"testing"
)

func TestPool_RegisterAndGet(t *testing.T) {
	p := New()
	p.Register("a1", "Explore", "search for patterns")

	info, ok := p.Get("a1")
	if !ok {
		t.Fatal("expected agent to be registered")
	}
	if info.Type != "Explore" {
		t.Errorf("type = %q, want Explore", info.Type)
	}
	if info.Status != StatusSpawning {
		t.Errorf("status = %v, want StatusSpawning", info.Status)
	}
}

func TestPool_Lifecycle(t *testing.T) {
	p := New()
	p.Register("a1", "Plan", "plan implementation")
	p.SetRunning("a1")

	info, _ := p.Get("a1")
	if info.Status != StatusRunning {
		t.Errorf("status = %v, want StatusRunning", info.Status)
	}

	p.RecordToolUse("a1", "read_file")
	p.RecordToolUse("a1", "grep_search")

	info, _ = p.Get("a1")
	if info.ToolUses != 2 {
		t.Errorf("tool_uses = %d, want 2", info.ToolUses)
	}

	p.RecordUsage("a1", 1000, 500)
	info, _ = p.Get("a1")
	if info.InputTokens != 1000 || info.OutputTokens != 500 {
		t.Errorf("tokens = (%d, %d), want (1000, 500)", info.InputTokens, info.OutputTokens)
	}

	p.Complete("a1", false)
	info, _ = p.Get("a1")
	if info.Status != StatusCompleted {
		t.Errorf("status = %v, want StatusCompleted", info.Status)
	}
	if info.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

func TestPool_ActiveAndAll(t *testing.T) {
	p := New()
	p.Register("a1", "Explore", "agent 1")
	p.SetRunning("a1")
	p.Register("a2", "Explore", "agent 2")
	p.SetRunning("a2")
	p.Register("a3", "Plan", "agent 3")
	p.Complete("a3", false)

	active := p.Active()
	if len(active) != 2 {
		t.Errorf("active count = %d, want 2", len(active))
	}

	all := p.All()
	if len(all) != 3 {
		t.Errorf("all count = %d, want 3", len(all))
	}

	if p.ActiveCount() != 2 {
		t.Errorf("ActiveCount = %d, want 2", p.ActiveCount())
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	p := New()
	p.Register("a1", "Explore", "test agent")
	p.SetRunning("a1")

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.RecordToolUse("a1", "grep_search")
			p.RecordUsage("a1", 10, 5)
		}()
	}
	wg.Wait()

	info, _ := p.Get("a1")
	if info.ToolUses != 100 {
		t.Errorf("tool_uses = %d, want 100", info.ToolUses)
	}
	if info.InputTokens != 1000 {
		t.Errorf("input_tokens = %d, want 1000", info.InputTokens)
	}
}

func TestPool_Remove(t *testing.T) {
	p := New()
	p.Register("a1", "Explore", "test")
	p.Remove("a1")

	_, ok := p.Get("a1")
	if ok {
		t.Error("expected agent to be removed")
	}
}

func TestAgentStatus_String(t *testing.T) {
	tests := []struct {
		s    AgentStatus
		want string
	}{
		{StatusSpawning, "spawning"},
		{StatusRunning, "running"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{AgentStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("AgentStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
