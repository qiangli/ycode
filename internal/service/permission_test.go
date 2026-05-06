package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// captureFirstPermReq subscribes, returns the first permission.request event,
// and immediately responds via svc to unblock the caller. Returns the captured
// event and the unsubscribe function.
func captureFirstPermReq(t *testing.T, b *bus.MemoryBus, svc *LocalService, allow bool) <-chan bus.Event {
	t.Helper()
	ch, unsub := b.Subscribe()
	out := make(chan bus.Event, 1)
	go func() {
		defer unsub()
		for ev := range ch {
			if ev.Type != bus.EventPermissionReq {
				continue
			}
			var payload struct {
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(ev.Data, &payload)
			_ = svc.RespondPermission(context.Background(), payload.RequestID, allow)
			out <- ev
			return
		}
	}()
	return out
}

func TestRequestPermission_PublishesAndUnblocks(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	app := &fakeApp{sessionID: "sess-1", workDir: t.TempDir()}
	svc := NewLocalService(app, memBus)

	captured := captureFirstPermReq(t, memBus, svc, true)

	allowed, err := svc.RequestPermission(context.Background(), "sess-1", "bash", "DangerFullAccess", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true")
	}

	select {
	case ev := <-captured:
		if ev.SessionID != "sess-1" {
			t.Errorf("session_id = %q", ev.SessionID)
		}
		var payload struct {
			Tool      string `json:"tool"`
			Mode      string `json:"mode"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Tool != "bash" || payload.Mode != "DangerFullAccess" {
			t.Errorf("payload = %+v", payload)
		}
		if payload.RequestID == "" {
			t.Error("request_id missing")
		}
	case <-time.After(time.Second):
		t.Fatal("permission.request event not captured")
	}
}

func TestRequestPermission_IncludesEditDetail(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &fakeApp{sessionID: "sess-2", workDir: dir}
	svc := NewLocalService(app, memBus)

	captured := captureFirstPermReq(t, memBus, svc, true)

	input := json.RawMessage(`{"file_path":"main.go","content":"package main\n\nfunc main() {}\n"}`)
	if _, err := svc.RequestPermission(context.Background(), "sess-2", "write_file", "WorkspaceWrite", input); err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}

	select {
	case ev := <-captured:
		var payload struct {
			Edit *EditDetail `json:"edit"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Edit == nil {
			t.Fatal("expected edit detail in payload")
		}
		if payload.Edit.FilePath != "main.go" {
			t.Errorf("file_path = %q", payload.Edit.FilePath)
		}
		if payload.Edit.BeforeText != "package main\n" {
			t.Errorf("before_text = %q", payload.Edit.BeforeText)
		}
		if payload.Edit.AfterText != "package main\n\nfunc main() {}\n" {
			t.Errorf("after_text = %q", payload.Edit.AfterText)
		}
	case <-time.After(time.Second):
		t.Fatal("permission.request event not captured")
	}
}

func TestRequestPermission_ContextCancel(t *testing.T) {
	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	app := &fakeApp{sessionID: "sess-x", workDir: t.TempDir()}
	svc := NewLocalService(app, memBus)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	allowed, err := svc.RequestPermission(ctx, "sess-x", "bash", "DangerFullAccess", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if allowed {
		t.Fatal("expected allowed=false on cancellation")
	}
}
