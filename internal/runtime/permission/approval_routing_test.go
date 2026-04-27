package permission

import (
	"context"
	"testing"
	"time"
)

func TestRequestApproval_Approved(t *testing.T) {
	router := NewApprovalRouter(5 * time.Second)
	req := ApprovalRequest{
		ID:          "req-1",
		ToolName:    "bash",
		Description: "run rm -rf /tmp/test",
		ChannelType: "cli",
		CreatedAt:   time.Now(),
	}

	go func() {
		// Simulate async approval.
		time.Sleep(50 * time.Millisecond)
		router.Respond(ApprovalResponse{RequestID: "req-1", Approved: true})
	}()

	approved, err := router.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Error("expected approval, got denial")
	}
}

func TestRequestApproval_Denied(t *testing.T) {
	router := NewApprovalRouter(5 * time.Second)
	req := ApprovalRequest{
		ID:          "req-2",
		ToolName:    "bash",
		Description: "run dangerous command",
		ChannelType: "telegram",
		CreatedAt:   time.Now(),
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		router.Respond(ApprovalResponse{RequestID: "req-2", Approved: false})
	}()

	approved, err := router.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Error("expected denial, got approval")
	}
}

func TestRequestApproval_Timeout(t *testing.T) {
	router := NewApprovalRouter(100 * time.Millisecond)
	req := ApprovalRequest{
		ID:          "req-3",
		ToolName:    "bash",
		Description: "nobody will respond",
		ChannelType: "cli",
		CreatedAt:   time.Now(),
	}

	approved, err := router.RequestApproval(context.Background(), req)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if approved {
		t.Error("expected false on timeout")
	}
}

func TestRespond_NonExistentRequest(t *testing.T) {
	router := NewApprovalRouter(5 * time.Second)

	ok := router.Respond(ApprovalResponse{RequestID: "does-not-exist", Approved: true})
	if ok {
		t.Error("expected false for non-existent request")
	}
}

func TestPendingCount(t *testing.T) {
	router := NewApprovalRouter(5 * time.Second)

	if router.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", router.PendingCount())
	}

	// Start a request in the background to add to pending.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.RequestApproval(ctx, ApprovalRequest{ID: "req-4", ToolName: "test"})
	}()

	// Wait for the request to register.
	time.Sleep(50 * time.Millisecond)

	if router.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", router.PendingCount())
	}

	// Cancel to clean up.
	cancel()
	<-done

	if router.PendingCount() != 0 {
		t.Errorf("expected 0 pending after cancel, got %d", router.PendingCount())
	}
}
