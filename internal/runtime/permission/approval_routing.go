package permission

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ApprovalRequest represents a pending approval for a dangerous tool operation.
type ApprovalRequest struct {
	ID          string // unique request ID
	ToolName    string // tool requiring approval
	Description string // human-readable description of the operation
	ChannelType string // originating platform (e.g., "telegram", "cli")
	ChannelID   string // platform-specific destination for the prompt
	CreatedAt   time.Time
}

// ApprovalResponse is the user's decision on an approval request.
type ApprovalResponse struct {
	RequestID string
	Approved  bool
}

// ApprovalRouter routes approval requests to the appropriate channel.
type ApprovalRouter struct {
	mu sync.Mutex
	// pending maps request ID to response channel.
	pending map[string]chan ApprovalResponse
	timeout time.Duration
}

// NewApprovalRouter creates a router with the given timeout for approval responses.
func NewApprovalRouter(timeout time.Duration) *ApprovalRouter {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &ApprovalRouter{
		pending: make(map[string]chan ApprovalResponse),
		timeout: timeout,
	}
}

// RequestApproval creates an approval request and waits for a response.
// For CLI, this blocks on stdin. For platforms, it sends to the channel and waits.
// Returns true if approved, false if denied or timed out.
func (r *ApprovalRouter) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	ch := make(chan ApprovalResponse, 1)

	r.mu.Lock()
	r.pending[req.ID] = ch
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, req.ID)
		r.mu.Unlock()
	}()

	// Wait for response with timeout.
	select {
	case resp := <-ch:
		return resp.Approved, nil
	case <-time.After(r.timeout):
		return false, fmt.Errorf("approval timed out after %v", r.timeout)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Respond delivers an approval response for a pending request.
// Returns false if no pending request with that ID exists.
func (r *ApprovalRouter) Respond(resp ApprovalResponse) bool {
	r.mu.Lock()
	ch, ok := r.pending[resp.RequestID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

// PendingCount returns the number of pending approval requests.
func (r *ApprovalRouter) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}
