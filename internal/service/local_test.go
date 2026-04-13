package service

import (
	"context"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// Tests use LocalService directly with nil app where possible.

func TestLocalService_Bus(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	svc := &LocalService{
		b:         memBus,
		cancels:   make(map[string]context.CancelFunc),
		permChans: make(map[string]chan bool),
	}

	if svc.Bus() != memBus {
		t.Error("Bus() should return the memory bus")
	}
}

func TestLocalService_CancelTurn(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	svc := &LocalService{
		b:         memBus,
		cancels:   make(map[string]context.CancelFunc),
		permChans: make(map[string]chan bool),
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc.cancelMu.Lock()
	svc.cancels["s1"] = cancel
	svc.cancelMu.Unlock()

	err := svc.CancelTurn(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		// Expected — context was cancelled.
	default:
		t.Error("context should be cancelled after CancelTurn")
	}
}

func TestLocalService_RespondPermission(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	svc := &LocalService{
		b:         memBus,
		cancels:   make(map[string]context.CancelFunc),
		permChans: make(map[string]chan bool),
	}

	ch := make(chan bool, 1)
	svc.permMu.Lock()
	svc.permChans["req-1"] = ch
	svc.permMu.Unlock()

	err := svc.RespondPermission(context.Background(), "req-1", true)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case allowed := <-ch:
		if !allowed {
			t.Error("expected allowed=true")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for permission response")
	}
}

func TestLocalService_RespondPermission_NotFound(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	svc := &LocalService{
		b:         memBus,
		cancels:   make(map[string]context.CancelFunc),
		permChans: make(map[string]chan bool),
	}

	err := svc.RespondPermission(context.Background(), "nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent request ID")
	}
}

func TestLocalService_GetStatus(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	// Can't easily test GetStatus without a real AppBackend, but we can
	// test that the struct is properly initialized.
	svc := NewLocalService(nil, memBus)
	if svc.Bus() != memBus {
		t.Error("Bus should return memBus")
	}
}
