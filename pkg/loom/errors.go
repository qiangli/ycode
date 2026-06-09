package loom

import "errors"

// Sentinel errors. Service methods return these (possibly wrapped) so
// callers can distinguish user errors from infrastructure failures
// without string-matching.
var (
	// ErrLeaseNotFound is returned when LoomID does not match any
	// active lease.
	ErrLeaseNotFound = errors.New("loom: lease not found")

	// ErrLeaseExpired is returned when a lease has been reaped or its
	// TTL has elapsed.
	ErrLeaseExpired = errors.New("loom: lease expired")

	// ErrInvalidRequest covers missing required fields (CWD,
	// SubAgentLabel, LoomID) and other input validation failures.
	ErrInvalidRequest = errors.New("loom: invalid request")

	// ErrQueueEmpty is returned by Service.Claim / Backend.ClaimNextIssue
	// when no candidate issue is available for the project. Callers
	// surface this as exit-3 ("nothing queued") in the v2 weave start
	// CLI per the agent-friendly conventions.
	ErrQueueEmpty = errors.New("loom: queue empty")
)
