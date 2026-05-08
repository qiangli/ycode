package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
)

func TestSlug_StableAcrossCalls(t *testing.T) {
	a := Slug("/Users/q/projects/ycode")
	b := Slug("/Users/q/projects/ycode")
	if a != b {
		t.Fatalf("slug not stable: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "ycode-") {
		t.Fatalf("expected slug to start with basename, got %q", a)
	}
}

func TestSlug_DifferentPathsProduceDifferentSlugs(t *testing.T) {
	a := Slug("/Users/q/projects/ycode")
	b := Slug("/Users/q/projects/ycode-fork")
	if a == b {
		t.Fatalf("expected different slugs for different paths, got %s", a)
	}
}

func TestSlug_SameBasenameDifferentPathDistinct(t *testing.T) {
	a := Slug("/a/foo")
	b := Slug("/b/foo")
	if a == b {
		t.Fatalf("collision: %s == %s", a, b)
	}
	if !strings.HasPrefix(a, "foo-") || !strings.HasPrefix(b, "foo-") {
		t.Fatalf("unexpected slugs %s %s", a, b)
	}
}

func TestRegistry_ResolveCreatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	cwd := t.TempDir()
	p, err := r.Resolve(context.Background(), cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Cwd != cwd {
		t.Errorf("Cwd: got %q want %q", p.Cwd, cwd)
	}
	if p.Slug == "" {
		t.Fatal("empty slug")
	}
	if p.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}

	// File should exist.
	data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("read projects.json: %v", err)
	}
	var list []*Project
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project on disk, got %d", len(list))
	}
}

func TestRegistry_ResolveIdempotent(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	cwd := t.TempDir()
	p1, _ := r.Resolve(context.Background(), cwd)
	p2, _ := r.Resolve(context.Background(), cwd)
	if p1 != p2 {
		t.Fatal("Resolve returned different pointers for same cwd")
	}
	if len(r.List()) != 1 {
		t.Fatalf("expected 1 project, got %d", len(r.List()))
	}
}

func TestRegistry_LoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	r1, _ := NewRegistry(dir)
	cwd := t.TempDir()
	if _, err := r1.Resolve(context.Background(), cwd); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	r2, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry reload: %v", err)
	}
	if r2.Get(cwd) == nil {
		t.Fatalf("expected reload to find existing project")
	}
}

func TestRegistry_MarkSynced(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	cwd := t.TempDir()
	r.Resolve(context.Background(), cwd)

	if err := r.MarkSynced(cwd); err != nil {
		t.Fatalf("MarkSynced: %v", err)
	}
	p := r.Get(cwd)
	if p.LastSync.IsZero() {
		t.Fatal("LastSync not set")
	}
	if time.Since(p.LastSync) > time.Minute {
		t.Errorf("LastSync looks stale: %v", p.LastSync)
	}
}

func TestEnsureRepo_CreatesWhenMissing(t *testing.T) {
	cwd := t.TempDir()
	r, _ := NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), cwd)

	createCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/api/v1/user/repos":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]gitserver.Repository{})
		case req.Method == "POST" && req.URL.Path == "/api/v1/user/repos":
			createCalled = true
			var body map[string]any
			json.NewDecoder(req.Body).Decode(&body)
			if body["name"] != p.Slug {
				t.Errorf("expected name=%q, got %v", p.Slug, body["name"])
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(gitserver.Repository{
				Name:     p.Slug,
				FullName: "admin/" + p.Slug,
			})
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer srv.Close()

	c := gitserver.NewClient(srv.URL, "test-token")
	created, err := EnsureRepo(context.Background(), c, p)
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if !created {
		t.Errorf("expected created=true")
	}
	if !createCalled {
		t.Errorf("expected POST /api/v1/user/repos to be called")
	}
}

func TestEnsureRepo_NoOpWhenExists(t *testing.T) {
	cwd := t.TempDir()
	r, _ := NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), cwd)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" {
			t.Errorf("unexpected POST: %s", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]gitserver.Repository{
			{Name: p.Slug, FullName: "admin/" + p.Slug},
		})
	}))
	defer srv.Close()

	c := gitserver.NewClient(srv.URL, "test-token")
	created, err := EnsureRepo(context.Background(), c, p)
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if created {
		t.Errorf("expected created=false when repo already exists")
	}
}

func TestSyncLog_AppendAndPending(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	p, _ := r.Resolve(context.Background(), t.TempDir())

	log, err := NewSyncLog(dir, p)
	if err != nil {
		t.Fatalf("NewSyncLog: %v", err)
	}
	sha := strings.Repeat("a", 40)
	if err := log.Append(SyncEntry{SHA: sha, PR: 7, AgentID: "agent-abc"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries, err := log.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SHA != sha || entries[0].PR != 7 || entries[0].AgentID != "agent-abc" {
		t.Errorf("entry mismatch: %+v", entries[0])
	}
}

func TestSyncLog_Truncate(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	p, _ := r.Resolve(context.Background(), t.TempDir())

	log, _ := NewSyncLog(dir, p)
	log.Append(SyncEntry{SHA: strings.Repeat("a", 40)})
	log.Append(SyncEntry{SHA: strings.Repeat("b", 40)})
	if err := log.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	entries, _ := log.Pending()
	if len(entries) != 0 {
		t.Errorf("expected 0 after truncate, got %d", len(entries))
	}
}

func TestSyncLog_AppendBadSHA(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	p, _ := r.Resolve(context.Background(), t.TempDir())

	log, _ := NewSyncLog(dir, p)
	if err := log.Append(SyncEntry{SHA: "short"}); err == nil {
		t.Errorf("expected error for short SHA")
	}
}
