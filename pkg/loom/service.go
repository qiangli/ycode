package loom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Options wires NewService to its dependencies. Backend and SandboxRoot
// are required; the rest have sensible defaults.
type Options struct {
	Backend     Backend
	Store       LeaseStore
	SandboxRoot string

	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	IdleTimeout    time.Duration
	ReaperInterval time.Duration

	// OnLeaseCwd, if non-nil, is invoked at most once per unique cwd
	// the first time a Lease for that cwd succeeds. The wiring layer
	// uses this to run cross-cutting initialization in the foreign
	// tool's project (e.g. selfinit). Errors and panics are caught
	// at the call site so the lease is never blocked.
	OnLeaseCwd func(ctx context.Context, cwd string)

	Logger *slog.Logger
	Now    func() time.Time
}

// Service is the public API of the loom substrate. Construct via
// NewService; close via Close to stop the reaper goroutine.
type Service struct {
	backend     Backend
	store       LeaseStore
	sandboxRoot string

	defaultTTL     time.Duration
	maxTTL         time.Duration
	idleTimeout    time.Duration
	reaperInterval time.Duration

	onLeaseCwd func(ctx context.Context, cwd string)

	log *slog.Logger
	now func() time.Time

	cancel       context.CancelFunc
	reaperDone   chan struct{}
	seenProjects sync.Map // slug -> struct{}
	seenCwds     sync.Map // cwd  -> struct{}
}

// NewService validates opts and returns a running Service. Caller must
// invoke Close when done to stop the reaper.
func NewService(opts Options) (*Service, error) {
	if opts.Backend == nil {
		return nil, fmt.Errorf("%w: nil Backend", ErrInvalidRequest)
	}
	if opts.SandboxRoot == "" {
		return nil, fmt.Errorf("%w: empty SandboxRoot", ErrInvalidRequest)
	}
	store := opts.Store
	if store == nil {
		store = NewMemoryStore()
	}
	defaultTTL := opts.DefaultTTL
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	maxTTL := opts.MaxTTL
	if maxTTL <= 0 {
		maxTTL = MaxTTL
	}
	if defaultTTL > maxTTL {
		defaultTTL = maxTTL
	}
	idleTimeout := opts.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	reaperInterval := opts.ReaperInterval
	if reaperInterval <= 0 {
		reaperInterval = DefaultReaperTick
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Service{
		backend:        opts.Backend,
		store:          store,
		sandboxRoot:    opts.SandboxRoot,
		defaultTTL:     defaultTTL,
		maxTTL:         maxTTL,
		idleTimeout:    idleTimeout,
		reaperInterval: reaperInterval,
		onLeaseCwd:     opts.OnLeaseCwd,
		log:            logger,
		now:            now,
		reaperDone:     make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.runReaper(ctx)

	return s, nil
}

// ReapNow runs one immediate reaper pass. Useful at startup to reclaim
// leases that outlived a previous `ycode serve`. Safe to call any time.
func (s *Service) ReapNow(ctx context.Context) {
	s.reapOnce(ctx)
}

// Close stops the reaper goroutine. Safe to call multiple times.
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.reaperDone
	return nil
}

// Lease provisions an isolated workspace for a sub-agent.
func (s *Service) Lease(ctx context.Context, req LeaseRequest) (Lease, error) {
	if req.CWD == "" {
		return Lease{}, fmt.Errorf("%w: CWD required", ErrInvalidRequest)
	}
	if req.SubAgentLabel == "" {
		return Lease{}, fmt.Errorf("%w: SubAgentLabel required", ErrInvalidRequest)
	}

	slug, cloneURL, err := s.backend.EnsureProject(ctx, req.CWD)
	if err != nil {
		return Lease{}, fmt.Errorf("loom: ensure project: %w", err)
	}

	// First time we've seen this cwd? Run the wiring-layer hook so
	// ycode can self-establish in the foreign tool's project. Wrapped
	// in defer/recover so a misbehaving callback never blocks a lease.
	if s.onLeaseCwd != nil {
		if _, loaded := s.seenCwds.LoadOrStore(req.CWD, struct{}{}); !loaded {
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.log.Warn("loom: OnLeaseCwd panicked", "cwd", req.CWD, "panic", r)
					}
				}()
				s.onLeaseCwd(ctx, req.CWD)
			}()
		}
	}

	// First time we've seen this project? Notify backend so it can
	// lazy-start per-project services (e.g. a merger).
	if _, loaded := s.seenProjects.LoadOrStore(slug, struct{}{}); !loaded {
		if err := s.backend.NotifyProjectActive(ctx, slug, cloneURL); err != nil {
			s.log.Warn("loom: NotifyProjectActive", "slug", slug, "err", err)
		}
	}

	agentID := newAgentID(req.SubAgentLabel)
	branch := newBranchName(agentID)
	authorName := agentID
	authorEmail := agentID + "@ycode.local"

	sandboxPath, err := s.backend.PrepareSandbox(ctx, s.sandboxRoot, slug, branch, agentID, authorName, authorEmail, cloneURL)
	if err != nil {
		return Lease{}, fmt.Errorf("loom: prepare sandbox: %w", err)
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	if ttl < MinTTL {
		ttl = MinTTL
	}
	if ttl > s.maxTTL {
		ttl = s.maxTTL
	}

	now := s.now()
	id := newLoomID()
	lease := Lease{
		ID:            id,
		Path:          sandboxPath,
		Branch:        branch,
		CloneURL:      cloneURL,
		AuthorName:    authorName,
		AuthorEmail:   authorEmail,
		ExpiresAt:     now.Add(ttl),
		Slug:          slug,
		SubAgentLabel: req.SubAgentLabel,
		AgentID:       agentID,
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := s.store.Put(lease); err != nil {
		// Best-effort cleanup of the sandbox we just created.
		_ = s.backend.DeleteSandbox(lease.Path)
		return Lease{}, fmt.Errorf("loom: store lease: %w", err)
	}
	return lease, nil
}

// Push commits and pushes the sub-agent's work in its sandbox.
func (s *Service) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	lease, err := s.touchLease(req.LoomID)
	if err != nil {
		return PushResult{}, err
	}
	msg := req.Message
	if msg == "" {
		msg = fmt.Sprintf("loom: %s", lease.SubAgentLabel)
	}
	sha, err := s.backend.CommitAndPush(ctx, lease.Path, lease.Slug, lease.Branch, msg, req.Force)
	if err != nil {
		return PushResult{}, fmt.Errorf("loom: commit and push: %w", err)
	}
	return PushResult{
		CommitSHA: sha,
		Branch:    lease.Branch,
		Pushed:    true,
	}, nil
}

// Merge opens (or returns) a PR from the lease's branch into main.
// The merger handles the actual merge once CI is green.
func (s *Service) Merge(ctx context.Context, req MergeRequest) (MergeResult, error) {
	lease, err := s.touchLease(req.LoomID)
	if err != nil {
		return MergeResult{}, err
	}

	// Idempotent: if a PR is already open for this lease, return its number.
	if lease.PRNumber > 0 {
		return MergeResult{PRNumber: lease.PRNumber, Status: "queued"}, nil
	}

	if err := s.backend.EnsureRemoteBranch(ctx, lease.Slug, lease.Branch); err != nil {
		return MergeResult{}, fmt.Errorf("loom: ensure remote branch: %w", err)
	}
	prNumber, err := s.backend.OpenPR(ctx, lease.Slug, lease.Branch, req.Title, req.Body)
	if err != nil {
		return MergeResult{}, fmt.Errorf("loom: open PR: %w", err)
	}
	lease.PRNumber = prNumber
	if err := s.store.Put(lease); err != nil {
		s.log.Warn("loom: persist lease PR number", "loom_id", lease.ID, "err", err)
	}
	return MergeResult{PRNumber: prNumber, Status: "queued"}, nil
}

// Status reports the state of zero or more leases.
func (s *Service) Status(ctx context.Context, req StatusRequest) ([]LeaseStatus, error) {
	all := s.store.List()

	// Filter by loom_id or cwd.
	var leases []Lease
	if req.LoomID != "" {
		l, ok := s.store.Get(req.LoomID)
		if !ok {
			return nil, ErrLeaseNotFound
		}
		leases = []Lease{l}
	} else if req.CWD != "" {
		// Resolve cwd to slug via backend so we don't have to keep cwd in lease.
		slug, _, err := s.backend.EnsureProject(ctx, req.CWD)
		if err != nil {
			return nil, fmt.Errorf("loom: status: ensure project: %w", err)
		}
		for _, l := range all {
			if l.Slug == slug {
				leases = append(leases, l)
			}
		}
	} else {
		leases = all
	}

	if len(leases) == 0 {
		return nil, nil
	}

	// Group leases by slug so we make at most one ListPRStates call per slug.
	bySlug := map[string][]Lease{}
	for _, l := range leases {
		bySlug[l.Slug] = append(bySlug[l.Slug], l)
	}
	prStateByBranch := map[string]BackendPRState{}
	for slug := range bySlug {
		states, err := s.backend.ListPRStates(ctx, slug, "agent/agent-"+SubAgentIDPrefix+"-")
		if err != nil {
			s.log.Warn("loom: list PR states", "slug", slug, "err", err)
			continue
		}
		for _, st := range states {
			prStateByBranch[st.HeadRef] = st
		}
	}

	out := make([]LeaseStatus, 0, len(leases))
	for _, l := range leases {
		st := LeaseStatus{
			LoomID: l.ID,
			Branch: l.Branch,
			State:  StateLeased,
		}
		if l.PRNumber > 0 {
			st.PRNumber = l.PRNumber
			st.State = StatePushed
		}
		if pr, ok := prStateByBranch[l.Branch]; ok {
			st.PRNumber = pr.PRNumber
			switch {
			case pr.Merged:
				st.State = StateMerged
			case pr.State == "closed":
				st.State = StateConflict
			default:
				st.State = StatePushed
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// Release tears down the sub-agent's sandbox. Best-effort: missing
// resources are not an error. If KeepBranch is false, the branch is
// also removed (open PRs are left intact — the merger will finish them).
func (s *Service) Release(ctx context.Context, req ReleaseRequest) error {
	lease, ok := s.store.Get(req.LoomID)
	if !ok {
		// Idempotent — already released or never existed.
		return nil
	}
	if err := s.backend.DeleteSandbox(lease.Path); err != nil {
		s.log.Warn("loom: delete sandbox", "loom_id", lease.ID, "err", err)
	}
	if !req.KeepBranch && lease.PRNumber == 0 {
		// Only delete the branch if no PR depends on it.
		if err := s.backend.DeleteBranch(ctx, lease.Slug, lease.Branch); err != nil {
			s.log.Warn("loom: delete branch", "loom_id", lease.ID, "err", err)
		}
	}
	if err := s.store.Delete(lease.ID); err != nil {
		return fmt.Errorf("loom: delete lease: %w", err)
	}
	return nil
}

// touchLease returns the lease for id, refreshing LastSeenAt. Returns
// ErrLeaseNotFound or ErrLeaseExpired as appropriate.
func (s *Service) touchLease(id string) (Lease, error) {
	if id == "" {
		return Lease{}, fmt.Errorf("%w: LoomID required", ErrInvalidRequest)
	}
	lease, ok := s.store.Get(id)
	if !ok {
		return Lease{}, ErrLeaseNotFound
	}
	now := s.now()
	if now.After(lease.ExpiresAt) {
		return Lease{}, ErrLeaseExpired
	}
	lease.LastSeenAt = now
	_ = s.store.Put(lease)
	return lease, nil
}

func (s *Service) runReaper(ctx context.Context) {
	defer close(s.reaperDone)
	t := time.NewTicker(s.reaperInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reapOnce(ctx)
		}
	}
}

func (s *Service) reapOnce(ctx context.Context) {
	now := s.now()
	for _, lease := range s.store.List() {
		expired := now.After(lease.ExpiresAt)
		idle := s.idleTimeout > 0 && now.Sub(lease.LastSeenAt) > s.idleTimeout
		if !expired && !idle {
			continue
		}
		s.log.Info("loom: reaping lease",
			"loom_id", lease.ID,
			"slug", lease.Slug,
			"expired", expired,
			"idle", idle,
		)
		if err := s.backend.DeleteSandbox(lease.Path); err != nil {
			s.log.Warn("loom: reaper delete sandbox", "loom_id", lease.ID, "err", err)
		}
		// Only delete the branch if the lease has no open PR — the
		// merger may still be working on it.
		if lease.PRNumber == 0 {
			if err := s.backend.DeleteBranch(ctx, lease.Slug, lease.Branch); err != nil {
				s.log.Warn("loom: reaper delete branch", "loom_id", lease.ID, "err", err)
			}
		}
		_ = s.store.Delete(lease.ID)
	}
}

// newAgentID returns a stable, label-prefixed identifier suitable for
// branch names and git author trailers. Format:
// "agent-loom-<sanitized-label>-<8 hex>". The "agent-loom-" prefix
// is the filter token used to separate foreign-driven work from
// ycode's own collab work in git log and OTel.
//
// The separator MUST stay ref-safe (git check-ref-format rejects ':',
// '?', '^', '~', '\', '*', and whitespace). Earlier versions used ':'
// which broke `git checkout -b` and `RefSpec` parsing in go-git.
func newAgentID(label string) string {
	clean := sanitizeLabel(label)
	if clean == "" {
		clean = "agent"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("agent-%s-%s-%s", SubAgentIDPrefix, clean, hex.EncodeToString(b[:]))
}

// newBranchName returns "agent/<id>/free-<rand>" — the same shape ycode's
// internal collab uses, so the existing merger and tooling treat loom
// branches uniformly.
func newBranchName(agentID string) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("agent/%s/free-%s", agentID, hex.EncodeToString(b[:]))
}

// newLoomID returns an opaque handle the foreign tool round-trips on
// every loom_* call.
func newLoomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "loom-" + hex.EncodeToString(b[:])
}

// sanitizeLabel keeps the label readable in branch names without
// breaking git. Allowed chars: letters, digits, '-', '_'. Everything
// else collapses to '-'. Leading/trailing dashes are trimmed.
func sanitizeLabel(s string) string {
	var sb strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_':
			sb.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				sb.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}
