package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// WorkspacePolicy controls how the server resolves a workDir when the
// client doesn't pin one explicitly via the request. It exists because
// the TUI's one-process-one-cwd model doesn't generalize: every web
// client hits the same `ycode serve` so a missing workDir would
// silently fall through to the server's cwd, which collides multi-tab
// and crosses user boundaries in multi-tenant deployments.
type WorkspacePolicy string

const (
	// PolicyPerSession (default) allocates a fresh empty workspace
	// directory under ~/.agents/ycode/workspaces/<owner>/<wsID>/ per
	// session-create. Safe for multi-tenant deployments — every
	// session gets a distinct VFS root and bash CWD.
	PolicyPerSession WorkspacePolicy = "per-session"

	// PolicyCWD reproduces the pre-policy behavior — every session
	// falls through to the server's startup os.Getwd(). Single-user
	// laptop dev only: sessions share files, so multi-user deployments
	// must NOT use this.
	PolicyCWD WorkspacePolicy = "cwd"

	// PolicyLoom leases a sandbox from the loom service per session.
	// The sandbox is a fresh clone of a configured project's repo with
	// its own branch + author identity. Opt-in; requires the loom
	// subsystem to be initialized. Wired separately.
	PolicyLoom WorkspacePolicy = "loom"
)

// Workspace is the resolved working-directory context the runtime
// will use. Workspaces are durable resources that outlive sessions
// (under per-session policy) — a returning user can reattach a new
// session to a previously-allocated workspace by ID.
type Workspace struct {
	// ID is the stable identifier. For per-session workspaces it's
	// the on-disk directory name; for cwd it's the literal "cwd"; for
	// loom it's the lease.ID.
	ID string

	// Owner is the bearer-token email when available, otherwise
	// "local". Path components are sanitized — see sanitizeOwner.
	Owner string

	// Path is the absolute on-disk path the runtime treats as its
	// CWD. The VFS allowed-dirs list narrows to {Path, tmp, otel}.
	Path string

	// CreatedAt is the workspace directory's mod time (per-session)
	// or the resolution time (cwd / loom). Used to sort the list
	// surface.
	CreatedAt time.Time
}

// ResolveHint is the per-request input to WorkspaceResolver.Resolve.
// The fields stack in priority: explicit work_dir wins outright;
// workspace_id reattaches an existing one; both empty falls through
// to the configured policy.
type ResolveHint struct {
	// ExplicitWorkDir mirrors today's POST /api/sessions {work_dir}.
	// When non-empty it short-circuits the policy — the caller knows
	// where they want to land.
	ExplicitWorkDir string

	// WorkspaceID names an existing per-session workspace to
	// reattach. The resolver verifies it exists on disk.
	WorkspaceID string

	// Owner is the authenticated identity. Empty falls back to
	// "local" for unauthenticated callers (--no-auth deployments).
	Owner string
}

// WorkspaceResolver decides what working directory a new session
// attaches to, based on policy + per-request hints. It also owns the
// list / delete surface over the on-disk workspace tree.
type WorkspaceResolver struct {
	policy WorkspacePolicy
	root   string // ~/.agents/ycode/workspaces
	cwd    string // captured at startup; used for PolicyCWD
	mu     sync.Mutex
}

// NewWorkspaceResolver wires a resolver. root is the parent dir
// under which per-session workspaces are allocated (typically
// ~/.agents/ycode/workspaces). cwd is the server's startup os.Getwd
// — captured once so PolicyCWD returns a stable value even if some
// goroutine later chdir's the process.
func NewWorkspaceResolver(policy WorkspacePolicy, root, cwd string) *WorkspaceResolver {
	if policy == "" {
		policy = PolicyPerSession
	}
	return &WorkspaceResolver{
		policy: policy,
		root:   root,
		cwd:    cwd,
	}
}

// Policy returns the configured policy (read-only).
func (r *WorkspaceResolver) Policy() WorkspacePolicy { return r.policy }

// Resolve returns a Workspace for a new (or reattaching) session.
func (r *WorkspaceResolver) Resolve(hint ResolveHint) (Workspace, error) {
	owner := sanitizeOwner(hint.Owner)

	// Explicit override: trust the caller. Today's behavior preserved
	// so existing clients keep working.
	if hint.ExplicitWorkDir != "" {
		abs, err := filepath.Abs(hint.ExplicitWorkDir)
		if err != nil {
			return Workspace{}, fmt.Errorf("resolve work_dir: %w", err)
		}
		return Workspace{
			ID:        "explicit",
			Owner:     owner,
			Path:      abs,
			CreatedAt: time.Now(),
		}, nil
	}

	// Reattach by workspace ID — verify it exists for THIS owner.
	if hint.WorkspaceID != "" {
		if !validID(hint.WorkspaceID) {
			return Workspace{}, fmt.Errorf("invalid workspace id: %q", hint.WorkspaceID)
		}
		path := r.workspaceDir(owner, hint.WorkspaceID)
		st, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Workspace{}, fmt.Errorf("workspace %q not found for %s", hint.WorkspaceID, owner)
			}
			return Workspace{}, fmt.Errorf("stat workspace: %w", err)
		}
		if !st.IsDir() {
			return Workspace{}, fmt.Errorf("workspace path is not a directory: %s", path)
		}
		return Workspace{
			ID:        hint.WorkspaceID,
			Owner:     owner,
			Path:      path,
			CreatedAt: st.ModTime(),
		}, nil
	}

	// Policy dispatch.
	switch r.policy {
	case PolicyCWD:
		return Workspace{
			ID:        "cwd",
			Owner:     owner,
			Path:      r.cwd,
			CreatedAt: time.Now(),
		}, nil
	case PolicyPerSession:
		return r.allocate(owner)
	case PolicyLoom:
		// Loom integration lands in a follow-up task. For now this
		// path is reachable only when the operator explicitly sets
		// --workspace-policy=loom AND nothing has wired in the loom
		// service; returning an error here is informative.
		return Workspace{}, fmt.Errorf("loom policy not yet wired; pass workspace_id or explicit work_dir")
	default:
		return Workspace{}, fmt.Errorf("unknown workspace policy: %q", r.policy)
	}
}

// allocate creates a fresh per-session workspace directory.
func (r *WorkspaceResolver) allocate(owner string) (Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Retry a few times on the off chance two allocations land in the
	// same second with colliding random suffixes. ID format is
	// timestamp + random so collisions are vanishingly rare; the
	// retry loop is paranoia.
	for range 4 {
		id := newWorkspaceID()
		path := r.workspaceDir(owner, id)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return Workspace{}, fmt.Errorf("create workspaces parent dir: %w", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return Workspace{}, fmt.Errorf("create workspace dir: %w", err)
		}
		return Workspace{
			ID:        id,
			Owner:     owner,
			Path:      path,
			CreatedAt: time.Now(),
		}, nil
	}
	return Workspace{}, fmt.Errorf("could not allocate unique workspace id after retries")
}

// List returns the existing workspaces on disk for the given owner.
// Sorted newest-first by directory mod time.
func (r *WorkspaceResolver) List(owner string) ([]Workspace, error) {
	owner = sanitizeOwner(owner)
	ownerDir := r.ownerDir(owner)
	entries, err := os.ReadDir(ownerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read owner dir: %w", err)
	}
	out := make([]Workspace, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Workspace{
			ID:        e.Name(),
			Owner:     owner,
			Path:      filepath.Join(ownerDir, e.Name()),
			CreatedAt: info.ModTime(),
		})
	}
	// Newest first — most-recently-touched is what the user is most
	// likely to want to reattach to.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// Delete removes a workspace from disk. The session pool is NOT
// notified — that's the caller's responsibility (see DELETE
// /api/workspaces in server.go).
func (r *WorkspaceResolver) Delete(owner, id string) error {
	owner = sanitizeOwner(owner)
	if !validID(id) {
		return fmt.Errorf("invalid workspace id: %q", id)
	}
	path := r.workspaceDir(owner, id)
	// Safety fence: never let an attacker craft an ID that resolves
	// outside the owner dir.
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	expectedPrefix, err := filepath.Abs(r.ownerDir(owner))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(abs, expectedPrefix+string(filepath.Separator)) {
		return fmt.Errorf("workspace path escapes owner dir: %s", abs)
	}
	return os.RemoveAll(path)
}

// CreateNew allocates a workspace without attaching a session —
// used by POST /api/workspaces so the user can prepare a workspace
// up front from the manage modal.
func (r *WorkspaceResolver) CreateNew(owner string) (Workspace, error) {
	return r.allocate(sanitizeOwner(owner))
}

func (r *WorkspaceResolver) ownerDir(owner string) string {
	return filepath.Join(r.root, owner)
}

func (r *WorkspaceResolver) workspaceDir(owner, id string) string {
	return filepath.Join(r.root, owner, id)
}

// newWorkspaceID generates a sortable, human-scannable identifier
// YYYYMMDD-HHMMSS-<6 hex>. Lexical order matches creation order,
// which makes operator-side `ls -1` listings useful.
func newWorkspaceID() string {
	now := time.Now().UTC()
	var randBytes [3]byte
	_, _ = rand.Read(randBytes[:])
	return fmt.Sprintf("%s-%s",
		now.Format("20060102-150405"),
		hex.EncodeToString(randBytes[:]),
	)
}

// sanitizeOwner maps a bearer-token email (or whatever identity the
// auth chain produces) to a safe path component. Anything outside
// [A-Za-z0-9._@+-] becomes "_". Empty input maps to "local" so
// --no-auth deployments still produce a stable owner directory
// instead of writing into the workspaces root.
func sanitizeOwner(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "local"
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '@', r == '+', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 128 {
		out = out[:128]
	}
	if out == "" {
		return "local"
	}
	return out
}

// validID is the gate on caller-supplied workspace IDs (reattach +
// delete). Keeps the resolver path-traversal-safe regardless of
// whether the auth chain has its own validation.
var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func validID(s string) bool { return idRe.MatchString(s) }
