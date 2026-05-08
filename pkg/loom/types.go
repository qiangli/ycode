package loom

import "time"

// LeaseRequest is the input to Service.Lease.
type LeaseRequest struct {
	// CWD is the absolute path of the foreign tool's host project.
	// Required. Used to resolve (and lazily create) the matching repo
	// in the underlying forge.
	CWD string `json:"cwd"`

	// SubAgentLabel is a short human-readable identifier the foreign
	// tool uses to distinguish its sub-agents (e.g. "extract-types",
	// "migrate-callers"). Required. Becomes part of the branch name
	// and author trailer so the work is traceable in git history.
	SubAgentLabel string `json:"sub_agent_label"`

	// TTLSeconds caps how long the lease can live before the reaper
	// reclaims it. Optional; clamped to [60, MaxTTLSeconds]. Zero
	// uses the service default.
	TTLSeconds int `json:"ttl_seconds,omitempty"`

	// BaseBranch is the source branch the lease's working branch
	// is cut from. Optional; defaults to "main".
	BaseBranch string `json:"base_branch,omitempty"`
}

// Lease is the handle returned by Service.Lease.
//
// The foreign tool passes ID back as loom_id on subsequent calls; the
// other fields are the substrate the sub-agent works inside. None of
// the fields besides ID need to round-trip through the foreign tool —
// the service stores the same data internally and reads ID alone is
// sufficient.
type Lease struct {
	ID          string    `json:"loom_id"`
	Path        string    `json:"path"`
	Branch      string    `json:"branch"`
	CloneURL    string    `json:"clone_url"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	ExpiresAt   time.Time `json:"expires_at"`

	// Internal bookkeeping — not part of the wire contract but
	// convenient for the LeaseStore. Exported so JSON-encoding
	// LeaseStore impls (FileStore) round-trip cleanly.
	Slug          string    `json:"slug"`
	SubAgentLabel string    `json:"sub_agent_label"`
	AgentID       string    `json:"agent_id"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	PRNumber      int64     `json:"pr_number,omitempty"`
}

// PushRequest is the input to Service.Push.
type PushRequest struct {
	LoomID  string `json:"loom_id"`
	Message string `json:"message,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

// PushResult is the output of Service.Push.
type PushResult struct {
	CommitSHA string `json:"commit_sha"`
	Branch    string `json:"branch"`
	Pushed    bool   `json:"pushed"`
}

// MergeRequest is the input to Service.Merge.
type MergeRequest struct {
	LoomID string `json:"loom_id"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
}

// MergeResult is the output of Service.Merge.
type MergeResult struct {
	PRNumber int64  `json:"pr_number"`
	Status   string `json:"status"`
}

// StatusRequest is the input to Service.Status. Either LoomID
// (specific lease) or CWD (all leases for a project) may be set.
// Empty matches every lease.
type StatusRequest struct {
	LoomID string `json:"loom_id,omitempty"`
	CWD    string `json:"cwd,omitempty"`
}

// LeaseStatus is one entry in the Service.Status reply.
type LeaseStatus struct {
	LoomID   string `json:"loom_id"`
	Branch   string `json:"branch"`
	State    string `json:"state"`
	PRNumber int64  `json:"pr_number,omitempty"`
	CITail   string `json:"ci_tail,omitempty"`
}

// Lease state values.
const (
	StateLeased   = "leased"
	StatePushed   = "pushed"
	StateMerging  = "merging"
	StateMerged   = "merged"
	StateCIFailed = "ci_failed"
	StateConflict = "conflict"
	StateReleased = "released"
)

// ReleaseRequest is the input to Service.Release.
type ReleaseRequest struct {
	LoomID     string `json:"loom_id"`
	KeepBranch bool   `json:"keep_branch,omitempty"`
}

// Default lifecycle constants. Service.NewService applies these when
// the corresponding Options field is zero.
const (
	DefaultTTL         = time.Hour
	MaxTTL             = 8 * time.Hour
	DefaultIdleTimeout = 30 * time.Minute
	DefaultReaperTick  = time.Minute
	MinTTL             = time.Minute
	SubAgentIDPrefix   = "loom"
)
