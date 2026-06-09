package loom

import (
	"context"
	"errors"
	"sync/atomic"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubBackend is an in-memory Backend for unit testing the service.
// All operations succeed by default; tests can hook the *Fn fields to
// inject failures.
type stubBackend struct {
	mu sync.Mutex

	cwdToSlug map[string]string

	sandboxes map[string]bool // path -> exists
	branches  map[string]bool // slug:branch -> exists
	prs       map[string]int64
	prStates  map[string]string // slug:branch -> state ("open"|"closed")
	merged    map[string]bool   // slug:branch -> true if merged
	prNumbers int64

	notifyCalls []string

	EnsureProjectFn  func(ctx context.Context, cwd string) (string, string, error)
	PrepareSandboxFn func(ctx context.Context, sandboxRoot, slug, branch, agentID, name, email, cloneURL string) (string, error)
	CommitAndPushFn  func(ctx context.Context, path, slug, branch, message string, force bool) (string, error)
	OpenPRFn         func(ctx context.Context, slug, branch, title, body string) (int64, error)
	NotifyProjectFn  func(ctx context.Context, slug, cloneURL string) error
	RebaseSandboxFn  func(ctx context.Context, sandboxPath, baseBranch string) ([]string, error)
	CheckpointFn     func(ctx context.Context, sandboxPath, message string) (string, error)
	ClaimNextIssueFn func(ctx context.Context, slug string) (int64, error)
}

func newStubBackend() *stubBackend {
	return &stubBackend{
		cwdToSlug: map[string]string{},
		sandboxes: map[string]bool{},
		branches:  map[string]bool{},
		prs:       map[string]int64{},
		prStates:  map[string]string{},
		merged:    map[string]bool{},
	}
}

func (b *stubBackend) EnsureProject(ctx context.Context, cwd string) (string, string, error) {
	if b.EnsureProjectFn != nil {
		return b.EnsureProjectFn(ctx, cwd)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	slug, ok := b.cwdToSlug[cwd]
	if !ok {
		slug = "p-" + filepath.Base(cwd)
		b.cwdToSlug[cwd] = slug
	}
	return slug, "http://stub/admin/" + slug + ".git", nil
}

func (b *stubBackend) PrepareSandbox(ctx context.Context, sandboxRoot, slug, branch, agentID, name, email, cloneURL string) (string, error) {
	if b.PrepareSandboxFn != nil {
		return b.PrepareSandboxFn(ctx, sandboxRoot, slug, branch, agentID, name, email, cloneURL)
	}
	path := filepath.Join(sandboxRoot, agentID)
	b.mu.Lock()
	b.sandboxes[path] = true
	b.mu.Unlock()
	return path, nil
}

func (b *stubBackend) CommitAndPush(ctx context.Context, path, slug, branch, message string, force bool) (string, error) {
	if b.CommitAndPushFn != nil {
		return b.CommitAndPushFn(ctx, path, slug, branch, message, force)
	}
	b.mu.Lock()
	b.branches[slug+":"+branch] = true
	b.prStates[slug+":"+branch] = "open"
	b.mu.Unlock()
	return "sha-" + branch, nil
}

func (b *stubBackend) EnsureRemoteBranch(ctx context.Context, slug, branch string) error {
	b.mu.Lock()
	b.branches[slug+":"+branch] = true
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) OpenPR(ctx context.Context, slug, branch, title, body string) (int64, error) {
	if b.OpenPRFn != nil {
		return b.OpenPRFn(ctx, slug, branch, title, body)
	}
	b.mu.Lock()
	b.prNumbers++
	n := b.prNumbers
	b.prs[slug+":"+branch] = n
	b.prStates[slug+":"+branch] = "open"
	b.mu.Unlock()
	return n, nil
}

func (b *stubBackend) ListPRStates(ctx context.Context, slug, branchPrefix string) ([]BackendPRState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []BackendPRState{}
	for key, n := range b.prs {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || parts[0] != slug {
			continue
		}
		branch := parts[1]
		if !strings.HasPrefix(branch, branchPrefix) {
			continue
		}
		out = append(out, BackendPRState{
			PRNumber: n,
			HeadRef:  branch,
			State:    b.prStates[key],
			Merged:   b.merged[key],
		})
	}
	return out, nil
}

func (b *stubBackend) DeleteSandbox(path string) error {
	b.mu.Lock()
	delete(b.sandboxes, path)
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) DeleteBranch(ctx context.Context, slug, branch string) error {
	b.mu.Lock()
	delete(b.branches, slug+":"+branch)
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) NotifyProjectActive(ctx context.Context, slug, cloneURL string) error {
	if b.NotifyProjectFn != nil {
		return b.NotifyProjectFn(ctx, slug, cloneURL)
	}
	b.mu.Lock()
	b.notifyCalls = append(b.notifyCalls, slug)
	b.mu.Unlock()
	return nil
}

func (b *stubBackend) RebaseSandbox(ctx context.Context, sandboxPath, baseBranch string) ([]string, error) {
	if b.RebaseSandboxFn != nil {
		return b.RebaseSandboxFn(ctx, sandboxPath, baseBranch)
	}
	return nil, nil
}

func (b *stubBackend) Checkpoint(ctx context.Context, sandboxPath, message string) (string, error) {
	if b.CheckpointFn != nil {
		return b.CheckpointFn(ctx, sandboxPath, message)
	}
	return "checkpoint-sha-" + message, nil
}

func (b *stubBackend) ClaimNextIssue(ctx context.Context, slug string) (int64, error) {
	if b.ClaimNextIssueFn != nil {
		return b.ClaimNextIssueFn(ctx, slug)
	}
	return 0, ErrQueueEmpty
}

// markMerged is a test helper to flip a branch to merged state.
func (b *stubBackend) markMerged(slug, branch string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.merged[slug+":"+branch] = true
	b.prStates[slug+":"+branch] = "closed"
}

func newTestService(t *testing.T) (*Service, *stubBackend) {
	t.Helper()
	backend := newStubBackend()
	svc, err := NewService(Options{
		Backend:        backend,
		SandboxRoot:    t.TempDir(),
		ReaperInterval: time.Hour, // never fires during tests; we call reapOnce by hand.
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, backend
}

func TestService_Lease_BasicHappyPath(t *testing.T) {
	svc, backend := newTestService(t)

	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD:           "/host/project",
		SubAgentLabel: "extract-types",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if lease.ID == "" || !strings.HasPrefix(lease.ID, "loom-") {
		t.Errorf("expected loom- prefix, got %q", lease.ID)
	}
	if lease.Slug != "p-project" {
		t.Errorf("Slug=%q want p-project", lease.Slug)
	}
	if !strings.HasPrefix(lease.Branch, "agent/agent-loom-extract-types-") {
		t.Errorf("Branch=%q does not match expected pattern", lease.Branch)
	}
	// Branch must be git-ref-valid: check-ref-format rejects ':' along
	// with '?', '^', '~', '\', '*', whitespace, and ".." sequences.
	for _, bad := range []string{":", "?", "^", "~", "\\", "*", " ", ".."} {
		if strings.Contains(lease.Branch, bad) {
			t.Errorf("Branch=%q contains ref-unsafe %q", lease.Branch, bad)
		}
	}
	if lease.Path == "" {
		t.Error("Path should be set")
	}
	if lease.AuthorName != lease.AgentID {
		t.Errorf("AuthorName=%q AgentID=%q", lease.AuthorName, lease.AgentID)
	}
	if !lease.ExpiresAt.After(lease.CreatedAt) {
		t.Errorf("ExpiresAt %v not after CreatedAt %v", lease.ExpiresAt, lease.CreatedAt)
	}

	// First lease for this slug fires NotifyProjectActive once.
	if got := len(backend.notifyCalls); got != 1 {
		t.Errorf("notifyCalls=%d want 1", got)
	}

	// Second lease for the same project should not re-notify.
	if _, err := svc.Lease(context.Background(), LeaseRequest{
		CWD:           "/host/project",
		SubAgentLabel: "migrate-callers",
	}); err != nil {
		t.Fatalf("second Lease: %v", err)
	}
	if got := len(backend.notifyCalls); got != 1 {
		t.Errorf("notifyCalls=%d want 1 after second lease", got)
	}
}

func TestService_Lease_RejectsMissingFields(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Lease(context.Background(), LeaseRequest{SubAgentLabel: "x"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("missing CWD: got %v want ErrInvalidRequest", err)
	}
	if _, err := svc.Lease(context.Background(), LeaseRequest{CWD: "/x"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("missing label: got %v want ErrInvalidRequest", err)
	}
}

func TestService_Lease_TTLClampedToMax(t *testing.T) {
	backend := newStubBackend()
	svc, err := NewService(Options{
		Backend:        backend,
		SandboxRoot:    t.TempDir(),
		MaxTTL:         2 * time.Minute,
		DefaultTTL:     time.Minute,
		ReaperInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()
	l, err := svc.Lease(context.Background(), LeaseRequest{
		CWD:           "/x",
		SubAgentLabel: "y",
		TTLSeconds:    99999, // way bigger than MaxTTL
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	got := l.ExpiresAt.Sub(l.CreatedAt)
	if got != 2*time.Minute {
		t.Errorf("TTL=%v want 2m (MaxTTL clamp)", got)
	}
}

func TestService_PushMergeStatus(t *testing.T) {
	svc, backend := newTestService(t)
	ctx := context.Background()

	lease, err := svc.Lease(ctx, LeaseRequest{CWD: "/host/p", SubAgentLabel: "edit"})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	push, err := svc.Push(ctx, PushRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !push.Pushed || push.CommitSHA == "" {
		t.Errorf("Push result: %+v", push)
	}

	merge, err := svc.Merge(ctx, MergeRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merge.PRNumber != 1 || merge.Status != "queued" {
		t.Errorf("Merge result: %+v", merge)
	}

	// Idempotent — second Merge returns the same number.
	merge2, err := svc.Merge(ctx, MergeRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Merge#2: %v", err)
	}
	if merge2.PRNumber != merge.PRNumber {
		t.Errorf("Merge not idempotent: %d vs %d", merge2.PRNumber, merge.PRNumber)
	}

	// Status while still open.
	statuses, err := svc.Status(ctx, StatusRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != StatePushed {
		t.Errorf("expected pushed: %+v", statuses)
	}

	// Mark the PR merged in the backend, then re-check status.
	backend.markMerged(lease.Slug, lease.Branch)
	statuses, err = svc.Status(ctx, StatusRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Status (post-merge): %v", err)
	}
	if statuses[0].State != StateMerged {
		t.Errorf("expected merged: %+v", statuses)
	}
}

func TestService_Status_ByCWD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	la, _ := svc.Lease(ctx, LeaseRequest{CWD: "/a", SubAgentLabel: "x"})
	lb1, _ := svc.Lease(ctx, LeaseRequest{CWD: "/b", SubAgentLabel: "y1"})
	lb2, _ := svc.Lease(ctx, LeaseRequest{CWD: "/b", SubAgentLabel: "y2"})

	statuses, err := svc.Status(ctx, StatusRequest{CWD: "/b"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 leases for /b, got %d", len(statuses))
	}
	gotIDs := map[string]bool{}
	for _, s := range statuses {
		gotIDs[s.LoomID] = true
	}
	if !gotIDs[lb1.ID] || !gotIDs[lb2.ID] {
		t.Errorf("missing /b leases: %+v", statuses)
	}
	if gotIDs[la.ID] {
		t.Errorf("statuses leaked /a's lease: %+v", statuses)
	}
}

func TestService_Status_LoomNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Status(context.Background(), StatusRequest{LoomID: "nope"}); !errors.Is(err, ErrLeaseNotFound) {
		t.Errorf("got %v want ErrLeaseNotFound", err)
	}
}

func TestService_Push_LeaseNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Push(context.Background(), PushRequest{LoomID: "nope"}); !errors.Is(err, ErrLeaseNotFound) {
		t.Errorf("got %v want ErrLeaseNotFound", err)
	}
}

func TestService_Push_LeaseExpired(t *testing.T) {
	backend := newStubBackend()
	now := time.Now()
	clock := &mockClock{t: now}
	svc, err := NewService(Options{
		Backend:        backend,
		SandboxRoot:    t.TempDir(),
		DefaultTTL:     time.Minute,
		ReaperInterval: time.Hour,
		Now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	lease, err := svc.Lease(context.Background(), LeaseRequest{CWD: "/x", SubAgentLabel: "y"})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	// Advance past TTL.
	clock.advance(2 * time.Minute)
	if _, err := svc.Push(context.Background(), PushRequest{LoomID: lease.ID}); !errors.Is(err, ErrLeaseExpired) {
		t.Errorf("got %v want ErrLeaseExpired", err)
	}
}

func TestService_Release_Idempotent(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	l, err := svc.Lease(ctx, LeaseRequest{CWD: "/x", SubAgentLabel: "y"})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if err := svc.Release(ctx, ReleaseRequest{LoomID: l.ID}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := svc.Release(ctx, ReleaseRequest{LoomID: l.ID}); err != nil {
		t.Fatalf("Release#2: %v", err)
	}
	// Underlying lease should be gone.
	if _, err := svc.Push(ctx, PushRequest{LoomID: l.ID}); !errors.Is(err, ErrLeaseNotFound) {
		t.Errorf("expected ErrLeaseNotFound after release, got %v", err)
	}
}

func TestService_Reaper_RemovesExpired(t *testing.T) {
	backend := newStubBackend()
	clock := &mockClock{t: time.Now()}
	svc, err := NewService(Options{
		Backend:        backend,
		SandboxRoot:    t.TempDir(),
		DefaultTTL:     time.Minute,
		ReaperInterval: time.Hour,
		IdleTimeout:    time.Hour,
		Now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()

	lease, err := svc.Lease(context.Background(), LeaseRequest{CWD: "/x", SubAgentLabel: "y"})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	// Sandbox exists right after lease.
	if !backend.sandboxes[lease.Path] {
		t.Fatal("sandbox should exist after lease")
	}

	// Advance past TTL and reap.
	clock.advance(2 * time.Minute)
	svc.reapOnce(context.Background())

	if backend.sandboxes[lease.Path] {
		t.Error("sandbox not deleted by reaper")
	}
	if _, ok := svc.store.Get(lease.ID); ok {
		t.Error("lease not deleted by reaper")
	}
}

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"extract-types":   "extract-types",
		"Migrate Callers": "Migrate-Callers",
		"a/b/c":           "a-b-c",
		"  hi  ":          "hi",
		"":                "",
		"emoji 🚀 nope":    "emoji-nope",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q)=%q want %q", in, got, want)
		}
	}
}

// mockClock is a manually-advanced time source for testing TTLs without
// real sleeps.
type mockClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *mockClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestService_Lease_PersistsBaseBranch(t *testing.T) {
	svc, _ := newTestService(t)
	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD:           "/host/project",
		SubAgentLabel: "label",
		BaseBranch:    "release-1.0",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if lease.BaseBranch != "release-1.0" {
		t.Errorf("BaseBranch=%q want release-1.0", lease.BaseBranch)
	}

	lease2, err := svc.Lease(context.Background(), LeaseRequest{
		CWD:           "/host/project",
		SubAgentLabel: "label2",
	})
	if err != nil {
		t.Fatalf("Lease (default base): %v", err)
	}
	if lease2.BaseBranch != DefaultBaseBranch {
		t.Errorf("BaseBranch=%q want %q", lease2.BaseBranch, DefaultBaseBranch)
	}
}

func TestService_Rebase_DelegatesToBackend(t *testing.T) {
	svc, backend := newTestService(t)
	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD: "/p", SubAgentLabel: "label",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	var gotBase, gotPath string
	backend.RebaseSandboxFn = func(_ context.Context, sandboxPath, baseBranch string) ([]string, error) {
		gotPath = sandboxPath
		gotBase = baseBranch
		return []string{"conflicted.go", "also/conflicted.go"}, nil
	}

	res, err := svc.Rebase(context.Background(), RebaseRequest{LoomID: lease.ID})
	if err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if gotBase != DefaultBaseBranch {
		t.Errorf("backend got base=%q want %q", gotBase, DefaultBaseBranch)
	}
	if gotPath != lease.Path {
		t.Errorf("backend got path=%q want %q", gotPath, lease.Path)
	}
	if len(res.ConflictFiles) != 2 || res.ConflictFiles[0] != "conflicted.go" {
		t.Errorf("ConflictFiles=%v", res.ConflictFiles)
	}
}

func TestService_SubmitAndWait_MergedHappyPath(t *testing.T) {
	svc, backend := newTestService(t)
	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD: "/host/project", SubAgentLabel: "label",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	// Stub merges as soon as the PR opens.
	backend.OpenPRFn = func(_ context.Context, slug, branch, _, _ string) (int64, error) {
		backend.mu.Lock()
		backend.prCounterBumpLocked()
		n := backend.prNumbers
		backend.prs[slug+":"+branch] = n
		backend.prStates[slug+":"+branch] = "closed"
		backend.merged[slug+":"+branch] = true
		backend.mu.Unlock()
		return n, nil
	}

	res, err := svc.SubmitAndWait(context.Background(), SubmitRequest{
		LoomID:         lease.ID,
		MaxWaitSeconds: 2,
	})
	if err != nil {
		t.Fatalf("SubmitAndWait: %v", err)
	}
	if res.State != StateMerged {
		t.Errorf("State=%q want %q", res.State, StateMerged)
	}
	if res.PRNumber == 0 {
		t.Errorf("PRNumber unset")
	}
}

func TestService_SubmitAndWait_PendingOnDeadline(t *testing.T) {
	svc, backend := newTestService(t)
	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD: "/host/project", SubAgentLabel: "label",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	// Backend never transitions the PR to closed/merged — caller hits
	// the wait deadline and returns pending.
	_ = backend
	res, err := svc.SubmitAndWait(context.Background(), SubmitRequest{
		LoomID:         lease.ID,
		MaxWaitSeconds: 1,
	})
	if err != nil {
		t.Fatalf("SubmitAndWait: %v", err)
	}
	if res.State != StatePending {
		t.Errorf("State=%q want %q", res.State, StatePending)
	}
}

// prCounterBumpLocked is a test helper to increment prNumbers under the
// caller's lock. Exposed so OpenPRFn overrides can mint sequential PR
// numbers without re-acquiring the mutex.
func (b *stubBackend) prCounterBumpLocked() {
	b.prNumbers++
}

func TestService_Watch_EmitsLeaseAndRelease(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := svc.Watch(ctx, WatchFilter{})

	lease, err := svc.Lease(context.Background(), LeaseRequest{
		CWD: "/host/project", SubAgentLabel: "label",
	})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.To != StateLeased {
			t.Errorf("first event To=%q want %q", ev.To, StateLeased)
		}
		if ev.LoomID != lease.ID {
			t.Errorf("first event LoomID=%q want %q", ev.LoomID, lease.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for leased event")
	}

	if err := svc.Release(context.Background(), ReleaseRequest{LoomID: lease.ID}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.To != StateReleased {
			t.Errorf("release event To=%q want %q", ev.To, StateReleased)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for released event")
	}
}

func TestService_Claim_EmptyQueue(t *testing.T) {
	svc, backend := newTestService(t)
	backend.ClaimNextIssueFn = func(_ context.Context, _ string) (int64, error) {
		return 0, ErrQueueEmpty
	}
	_, err := svc.Claim(context.Background(), ClaimRequest{Slug: "myapp"})
	if !errors.Is(err, ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestService_Claim_RequiresSlug(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Claim(context.Background(), ClaimRequest{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestService_Claim_SerializesConcurrentCalls(t *testing.T) {
	svc, backend := newTestService(t)

	// Hand out issue numbers 1, 2, 3 in order, asserting that the
	// backend is never called twice in parallel by gating on a strict
	// mutex inside the stub. If Service.Claim's per-slug mutex works
	// correctly, the stub's in-flight count never exceeds 1.
	var inFlight int32
	var counter int64
	backend.ClaimNextIssueFn = func(_ context.Context, _ string) (int64, error) {
		n := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		if n > 1 {
			t.Errorf("ClaimNextIssue called concurrently: in-flight=%d", n)
		}
		// Small delay to make a missing lock actually fail.
		time.Sleep(10 * time.Millisecond)
		return atomic.AddInt64(&counter, 1), nil
	}

	const N = 8
	var wg sync.WaitGroup
	results := make([]int64, N)
	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := svc.Claim(context.Background(), ClaimRequest{Slug: "myapp"})
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}
			results[idx] = res.IssueNumber
		}(i)
	}
	wg.Wait()

	// Every issue number must be distinct (1..N), confirming each
	// concurrent caller got a different one.
	seen := map[int64]bool{}
	for _, n := range results {
		if seen[n] {
			t.Errorf("duplicate issue %d claimed", n)
		}
		seen[n] = true
	}
}

func TestService_Watch_FilterByLoomID(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe BEFORE creating leases, with a filter for a specific
	// loom_id we'll learn once the matching lease is created.
	a, err := svc.Lease(context.Background(), LeaseRequest{CWD: "/p1", SubAgentLabel: "a"})
	if err != nil {
		t.Fatalf("Lease A: %v", err)
	}
	ch := svc.Watch(ctx, WatchFilter{LoomID: a.ID})

	// Now create a second lease — the filtered watcher should NOT see it.
	_, err = svc.Lease(context.Background(), LeaseRequest{CWD: "/p2", SubAgentLabel: "b"})
	if err != nil {
		t.Fatalf("Lease B: %v", err)
	}

	// Release A; that event MUST arrive on the filtered channel.
	if err := svc.Release(context.Background(), ReleaseRequest{LoomID: a.ID}); err != nil {
		t.Fatalf("Release A: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.LoomID != a.ID {
			t.Errorf("event for wrong loom_id: %q (want %q)", ev.LoomID, a.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for A's release event")
	}
}
