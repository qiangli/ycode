package service

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
)

func TestMultiService_CreateSession_EchoesOptions(t *testing.T) {
	pool := NewSessionPool(func(workDir string) (AppBackend, error) {
		return &fakeApp{sessionID: "fake-id", workDir: workDir, model: "default-model"}, nil
	})
	defer pool.Close()

	memBus := bus.NewMemoryBus()
	defer memBus.Close()
	multi := NewMultiService(pool, memBus)

	opts := SessionOptions{
		Model:           "claude-haiku-4-5-20251001",
		ToolsAllowlist:  []string{"read_file", "grep_search"},
		PersonaDisabled: true,
	}
	ctx := context.WithValue(context.Background(), CtxWorkDir, "/tmp/x")
	ctx = context.WithValue(ctx, CtxSessionOptions, opts)

	info, err := multi.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if info.Options == nil {
		t.Fatal("expected SessionInfo.Options to be populated")
	}
	if info.Options.Model != opts.Model {
		t.Errorf("Options.Model = %q, want %q", info.Options.Model, opts.Model)
	}
	if !info.Options.PersonaDisabled {
		t.Error("Options.PersonaDisabled lost")
	}
	if len(info.Options.ToolsAllowlist) != 2 {
		t.Errorf("Options.ToolsAllowlist len = %d, want 2", len(info.Options.ToolsAllowlist))
	}

	// Round-trip via GetSession.
	got, err := multi.GetSession(ctx, info.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Options == nil || got.Options.Model != opts.Model {
		t.Errorf("GetSession lost options: %+v", got.Options)
	}
}

func TestMultiService_CreateSession_ZeroOptionsOmitted(t *testing.T) {
	pool := NewSessionPool(func(workDir string) (AppBackend, error) {
		return &fakeApp{sessionID: "fake-id-2", workDir: workDir}, nil
	})
	defer pool.Close()

	memBus := bus.NewMemoryBus()
	defer memBus.Close()
	multi := NewMultiService(pool, memBus)

	ctx := context.WithValue(context.Background(), CtxWorkDir, "/tmp/y")
	info, err := multi.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if info.Options != nil {
		t.Errorf("expected SessionInfo.Options to be nil for zero options, got %+v", info.Options)
	}
}

func TestSessionOptions_IsZero(t *testing.T) {
	if !(SessionOptions{}.IsZero()) {
		t.Error("zero SessionOptions reported as non-zero")
	}
	cases := []SessionOptions{
		{Model: "x"},
		{ToolsAllowlist: []string{"a"}},
		{ToolsBlocklist: []string{"a"}},
		{PersonaDisabled: true},
	}
	for _, c := range cases {
		if c.IsZero() {
			t.Errorf("non-zero SessionOptions reported as zero: %+v", c)
		}
	}
}
