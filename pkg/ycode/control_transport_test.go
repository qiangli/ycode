package ycode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/harness/event"
)

func TestControlOwnerDeathAfterRequest(t *testing.T) {
	if source := os.Getenv("YCODE_TEST_DYING_OWNER"); source != "" {
		h, err := Load(source)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := h.lockSession("dying-owner")
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
		_, err = h.events.Append(event.Draft{SessionID: "dying-owner", RunID: "owned", Type: "session.requested", ConfigDigest: h.doc.ConfigDigest})
		if err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	h, _ := loadQueueHarness(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestControlOwnerDeathAfterRequest$")
	cmd.Env = append(os.Environ(), "YCODE_TEST_DYING_OWNER="+h.doc.Source)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	waitEvent := func(kind string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			items, err := event.Replay(h.eventPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.Type == kind {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("missing %s", kind)
	}
	waitEvent("session.requested")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Pause(ctx, "dying-owner") }()
	waitEvent("session.control-requested")
	select {
	case err := <-done:
		t.Fatalf("returned while owner held lock: %v", err)
	default:
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "owner lost") {
			t.Fatalf("want owner-lost error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control hung after owner death")
	}
	items, err := event.Replay(h.eventPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Type == "session.control-acknowledged" || item.Type == "session.run-finished" {
			t.Fatalf("unexpected completion: %s", item.Type)
		}
	}
}

func TestControlOwnerFilesystemError(t *testing.T) {
	h, _ := loadQueueHarness(t)
	// A file in place of the directory fails even when tests run as root.
	h.control = filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(h.control, nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := h.checkControlOwner("session")
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !strings.Contains(err.Error(), "check owning session lock") {
		t.Fatalf("want filesystem error, got %v", err)
	}
}
