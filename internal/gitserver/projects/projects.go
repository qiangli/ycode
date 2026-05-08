// Package projects manages the mapping between host project directories
// (the user's cwd) and repos in ycode's embedded Gitea. It owns the
// "upstream is a mirror of cwd" invariant and the per-project metadata
// stored in projects.json under the Gitea data dir.
//
// Layout (single-repo model — see docs/agent-collab.md):
//
//	admin/<slug>          // tracking repo: branch main mirrors cwd HEAD,
//	                      //                branches agent/<id>/* are work,
//	                      //                issues are the work queue
package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
)

// Owner is the Gitea user that owns all per-project repos.
// Single-user mode: the admin token's user owns everything.
const Owner = "admin"

// Project is the resolved mapping between an absolute cwd and its
// internal Gitea repo.
type Project struct {
	Cwd       string    `json:"cwd"`       // absolute path to host project
	Slug      string    `json:"slug"`      // <basename>-<8-char-hash>
	CreatedAt time.Time `json:"createdAt"` // first time we saw this cwd
	LastSync  time.Time `json:"lastSync"`  // last successful mirror push
}

// Repo returns the Gitea full name (owner/repo) for the project.
func (p *Project) Repo() string {
	return fmt.Sprintf("%s/%s", Owner, p.Slug)
}

// Slug returns a stable slug for the given absolute path.
// Two checkouts of the same logical repo at different paths
// get distinct slugs.
func Slug(absCwd string) string {
	base := filepath.Base(absCwd)
	base = sanitize(base)
	if base == "" {
		base = "project"
	}
	sum := sha256.Sum256([]byte(absCwd))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:4]))
}

var slugAllowed = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitize(s string) string {
	return strings.Trim(slugAllowed.ReplaceAllString(s, "-"), "-")
}

// Registry persists the cwd→Project mapping in projects.json.
//
// File location: <giteaDataDir>/projects.json (e.g. ~/.agents/ycode/gitea/projects.json).
// Concurrent access is serialized by the embedded mutex; the file itself
// is rewritten atomically via a temp file + rename.
type Registry struct {
	path string

	mu       sync.Mutex
	projects map[string]*Project // key: absolute cwd
}

// NewRegistry opens (or creates) the registry rooted at giteaDataDir.
func NewRegistry(giteaDataDir string) (*Registry, error) {
	if giteaDataDir == "" {
		return nil, fmt.Errorf("projects: empty giteaDataDir")
	}
	r := &Registry{
		path:     filepath.Join(giteaDataDir, "projects.json"),
		projects: make(map[string]*Project),
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// Resolve looks up an existing project for cwd or creates a new one.
// The returned *Project is the same pointer stored in the registry, so
// callers may read fields without further locking.
func (r *Registry) Resolve(ctx context.Context, cwd string) (*Project, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("projects: resolve abs: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.projects[abs]; ok {
		return p, nil
	}
	p := &Project{
		Cwd:       abs,
		Slug:      Slug(abs),
		CreatedAt: time.Now().UTC(),
	}
	r.projects[abs] = p
	if err := r.saveLocked(); err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns a project by cwd if known, else nil.
func (r *Registry) Get(cwd string) *Project {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.projects[abs]
}

// List returns a snapshot of all known projects.
func (r *Registry) List() []*Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		out = append(out, p)
	}
	return out
}

// MarkSynced records that cwd's HEAD has been mirrored to upstream.
func (r *Registry) MarkSynced(cwd string) error {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.projects[abs]
	if !ok {
		return fmt.Errorf("projects: unknown cwd %q", abs)
	}
	p.LastSync = time.Now().UTC()
	return r.saveLocked()
}

func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("projects: read %s: %w", r.path, err)
	}
	var list []*Project
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("projects: decode %s: %w", r.path, err)
	}
	for _, p := range list {
		r.projects[p.Cwd] = p
	}
	return nil
}

func (r *Registry) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	list := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		list = append(list, p)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// EnsureRepo makes sure admin/<slug> exists in Gitea. Idempotent.
// Returns (created, error) — created=true if it was newly created.
func EnsureRepo(ctx context.Context, c *gitserver.Client, p *Project) (bool, error) {
	repos, err := c.ListRepos(ctx)
	if err != nil {
		return false, fmt.Errorf("projects: list repos: %w", err)
	}
	want := p.Slug
	for _, r := range repos {
		if r.Name == want {
			return false, nil
		}
	}
	desc := fmt.Sprintf("ycode tracking repo for %s", p.Cwd)
	if _, err := c.CreateRepo(ctx, want, desc); err != nil {
		return false, fmt.Errorf("projects: create repo %s: %w", want, err)
	}
	return true, nil
}
