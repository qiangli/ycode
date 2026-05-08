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
)
