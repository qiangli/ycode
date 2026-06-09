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

	watchers *watcherSet
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
		watchers:       newWatcherSet(),
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
	if s.watchers != nil {
		s.watchers.closeAll()
	}
	return nil
}

// Watch returns a channel of LeaseEvents filtered by the given filter.
// The channel is closed when ctx is cancelled or Service.Close runs.
// Slow consumers drop events rather than blocking the emitter; the
// caller is responsible for keeping up.
func (s *Service) Watch(ctx context.Context, filter WatchFilter) <-chan LeaseEvent {
	ch, _ := s.watchers.subscribe(ctx, filter)
	return ch
}

// emitEvent is the internal helper used by mutating methods to publish
// a state transition. Safe to call with watchers nil (no-op).
func (s *Service) emitEvent(lease Lease, from, to string, extra map[string]any) {
	if s.watchers == nil {
		return
	}
	s.watchers.emit(LeaseEvent{
		LoomID:    lease.ID,
		Slug:      lease.Slug,
		Branch:    lease.Branch,
		From:      from,
		To:        to,
		Timestamp: s.now(),
		Extra:     extra,
	})
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

	baseBranch := req.BaseBranch
	if baseBranch == "" {
		baseBranch = DefaultBaseBranch
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
		BaseBranch:    baseBranch,
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := s.store.Put(lease); err != nil {
		// Best-effort cleanup of the sandbox we just created.
		_ = s.backend.DeleteSandbox(lease.Path)
		return Lease{}, fmt.Errorf("loom: store lease: %w", err)
	}
	s.emitEvent(lease, "", StateLeased, nil)
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

// Checkpoint stages and commits everything in the lease's sandbox
// without pushing — a lightweight save point before risky edits. The
// v2 loom_checkpoint verb dispatches here. Idempotent at the backend
// layer: empty staging area returns the current HEAD SHA with
// HadNoChanges=true.
func (s *Service) Checkpoint(ctx context.Context, req CheckpointRequest) (CheckpointResult, error) {
	lease, err := s.touchLease(req.LoomID)
	if err != nil {
		return CheckpointResult{}, err
	}
	msg := req.Summary
	if msg == "" {
		msg = fmt.Sprintf("loom: checkpoint (%s)", lease.SubAgentLabel)
	}
	priorSHA, _ := s.headSHA(ctx, lease.Path) // best-effort; pre-commit HEAD
	sha, err := s.backend.Checkpoint(ctx, lease.Path, msg)
	if err != nil {
		return CheckpointResult{}, fmt.Errorf("loom: checkpoint: %w", err)
	}
	res := CheckpointResult{
		CheckpointID: sha,
		CommitSHA:    sha,
	}
	if priorSHA != "" && priorSHA == sha {
		res.HadNoChanges = true
	}
	return res, nil
}

// headSHA reads the current HEAD SHA of a sandbox via a captureGit-
// equivalent path. Returns empty string on error — caller treats that
// as "couldn't compare" rather than failing the whole checkpoint.
//
// The implementation lives behind the backend so pkg/loom stays free
// of git-CLI imports; for backends that don't expose this directly,
// the comparison just degrades to "always assume changes were made."
func (s *Service) headSHA(_ context.Context, _ string) (string, error) {
	// Placeholder: a future PR can extend Backend with HeadSHA(path) if
	// the HadNoChanges signal becomes load-bearing. Today the field is
	// best-effort metadata for the agent's UX.
	return "", nil
}

// Rebase runs `git fetch origin <base> && git rebase origin/<base>`
// inside the sandbox via the backend. Conflict markers are left in
// place; the agent edits the files and resubmits. Returns the list of
// conflicted files (empty on clean rebase).
func (s *Service) Rebase(ctx context.Context, req RebaseRequest) (RebaseResult, error) {
	lease, err := s.touchLease(req.LoomID)
	if err != nil {
		return RebaseResult{}, err
	}
	base := lease.BaseBranch
	if base == "" {
		base = DefaultBaseBranch
	}
	files, err := s.backend.RebaseSandbox(ctx, lease.Path, base)
	if err != nil {
		return RebaseResult{}, fmt.Errorf("loom: rebase: %w", err)
	}
	return RebaseResult{ConflictFiles: files}, nil
}

// SubmitAndWait is the v2 sub-agent submit verb. Push + open/refresh PR
// + block until terminal state, automatically rebasing-in-place if the
// merger detects a conflict. The single call replaces the v1
// push → merge → poll-status orchestration the sub-agent used to do
// by hand.
//
// Terminal states returned:
//   - StateMerged     — PR merged; lease's work is on main.
//   - StateConflict   — rebase produced conflicts; ConflictFiles is set.
//   - StateCIFailed   — PR closed unmerged with no conflict files.
//   - StatePending    — wait deadline hit before terminal; caller may
//                       resubmit to keep waiting.
//
// Idempotent on PR creation: if the lease already has a PR open, that
// PR is reused (CommitAndPush still runs to push any new commits).
func (s *Service) SubmitAndWait(ctx context.Context, req SubmitRequest) (SubmitResult, error) {
	lease, err := s.touchLease(req.LoomID)
	if err != nil {
		return SubmitResult{}, err
	}

	// 1. Commit (if staged changes exist) and push.
	msg := req.Message
	if msg == "" {
		msg = fmt.Sprintf("loom: %s", lease.SubAgentLabel)
	}
	sha, err := s.backend.CommitAndPush(ctx, lease.Path, lease.Slug, lease.Branch, msg, req.Force)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("loom: commit and push: %w", err)
	}

	// 2. Open PR (or reuse existing).
	if err := s.backend.EnsureRemoteBranch(ctx, lease.Slug, lease.Branch); err != nil {
		return SubmitResult{}, fmt.Errorf("loom: ensure remote branch: %w", err)
	}
	prNumber := lease.PRNumber
	if prNumber == 0 {
		prNumber, err = s.backend.OpenPR(ctx, lease.Slug, lease.Branch, req.Title, req.Body)
		if err != nil {
			return SubmitResult{}, fmt.Errorf("loom: open PR: %w", err)
		}
		lease.PRNumber = prNumber
		if perr := s.store.Put(lease); perr != nil {
			s.log.Warn("loom: persist lease PR number", "loom_id", lease.ID, "err", perr)
		}
		s.emitEvent(lease, StateLeased, StatePushed, map[string]any{"pr": prNumber, "sha": sha})
	}

	// 3. Block until terminal state or wait deadline.
	wait := time.Duration(req.MaxWaitSeconds) * time.Second
	if wait <= 0 {
		wait = DefaultSubmitMaxWait
	}
	deadline := s.now().Add(wait)
	poll := DefaultSubmitPollInterval

	for {
		states, perr := s.backend.ListPRStates(ctx, lease.Slug, lease.Branch)
		if perr == nil {
			for _, st := range states {
				if st.HeadRef != lease.Branch {
					continue
				}
				switch {
				case st.Merged:
					s.emitEvent(lease, StatePushed, StateMerged, map[string]any{"pr": prNumber})
					return SubmitResult{
						State:     StateMerged,
						PRNumber:  prNumber,
						CommitSHA: sha,
					}, nil
				case st.State == "closed":
					// Closed without merge — either CI failure or
					// unresolvable conflict. Probe with a rebase to
					// distinguish: if rebase yields conflict files,
					// surface conflict; else treat as ci_failed.
					rb, rerr := s.backend.RebaseSandbox(ctx, lease.Path, leaseBase(lease))
					if rerr == nil && len(rb) > 0 {
						s.emitEvent(lease, StatePushed, StateConflict, map[string]any{"pr": prNumber, "files": rb})
						return SubmitResult{
							State:         StateConflict,
							PRNumber:      prNumber,
							CommitSHA:     sha,
							ConflictFiles: rb,
						}, nil
					}
					s.emitEvent(lease, StatePushed, StateCIFailed, map[string]any{"pr": prNumber})
					return SubmitResult{
						State:     StateCIFailed,
						PRNumber:  prNumber,
						CommitSHA: sha,
					}, nil
				}
			}
		} else {
			s.log.Warn("loom: submit poll: ListPRStates", "loom_id", lease.ID, "err", perr)
		}

		// Not yet terminal — check deadline, then sleep.
		if !s.now().Before(deadline) {
			return SubmitResult{
				State:     StatePending,
				PRNumber:  prNumber,
				CommitSHA: sha,
			}, nil
		}
		select {
		case <-ctx.Done():
			return SubmitResult{
				State:     StatePending,
				PRNumber:  prNumber,
				CommitSHA: sha,
			}, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// leaseBase returns the lease's base branch with a sensible fallback,
// used by SubmitAndWait when it needs to invoke rebase without a
// separate Service.Rebase call (skips the touchLease bookkeeping).
func leaseBase(lease Lease) string {
	if lease.BaseBranch != "" {
		return lease.BaseBranch
	}
	return DefaultBaseBranch
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
	s.emitEvent(lease, "", StateReleased, nil)
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
		s.emitEvent(lease, "", StateReleased, map[string]any{"reaper": true})
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
