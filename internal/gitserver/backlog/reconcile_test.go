//go:build experimental

package backlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// fakeGitea is a minimal in-memory Gitea-shaped server. Mirrors the
// pattern in queue/queue_test.go but kept package-local to avoid
// coupling the two test fixtures.
type fakeGitea struct {
	t      *testing.T
	srv    *httptest.Server
	owner  string
	repo   string
	issues []gitserver.Issue
	labels []gitserver.Label
	nextID int64
}

func newFakeGitea(t *testing.T, owner, repo string) *fakeGitea {
	t.Helper()
	f := &fakeGitea{t: t, owner: owner, repo: repo, nextID: 1}
	mux := http.NewServeMux()
	prefix := "/api/v1/repos/" + owner + "/" + repo

	mux.HandleFunc(prefix+"/labels", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, f.labels)
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			l := gitserver.Label{ID: f.nextID, Name: body["name"].(string), Color: stringFrom(body["color"])}
			f.nextID++
			f.labels = append(f.labels, l)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, l)
		}
	})

	mux.HandleFunc(prefix+"/issues", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			state := r.URL.Query().Get("state")
			out := []gitserver.Issue{}
			for _, i := range f.issues {
				if state == "" || state == "all" || i.State == state {
					out = append(out, i)
				}
			}
			writeJSON(w, out)
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			n := int64(len(f.issues) + 1)
			i := gitserver.Issue{
				ID:     f.nextID,
				Number: n,
				Title:  stringFrom(body["title"]),
				Body:   stringFrom(body["body"]),
				State:  "open",
			}
			f.nextID++
			f.issues = append(f.issues, i)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, i)
		}
	})

	mux.HandleFunc(prefix+"/issues/", func(w http.ResponseWriter, r *http.Request) {
		// /issues/<number> — accept GET and PATCH.
		var n int
		_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, prefix+"/issues/"), "%d", &n)
		idx := -1
		for i := range f.issues {
			if int(f.issues[i].Number) == n {
				idx = i
				break
			}
		}
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, f.issues[idx])
		case http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if v, ok := body["title"].(string); ok {
				f.issues[idx].Title = v
			}
			if v, ok := body["body"].(string); ok {
				f.issues[idx].Body = v
			}
			if v, ok := body["state"].(string); ok {
				f.issues[idx].State = v
			}
			if v, ok := body["labels"].([]any); ok {
				labels := []gitserver.Label{}
				for _, raw := range v {
					id, ok := raw.(float64)
					if !ok {
						continue
					}
					for _, l := range f.labels {
						if l.ID == int64(id) {
							labels = append(labels, l)
							break
						}
					}
				}
				f.issues[idx].Labels = labels
			}
			writeJSON(w, f.issues[idx])
		}
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func stringFrom(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestSplitFrontmatter(t *testing.T) {
	in := []byte("---\ntitle: hello\npriority: p1\n---\nbody text\n")
	fm, body, err := splitFrontmatter(in)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if !strings.Contains(string(fm), "title: hello") {
		t.Errorf("frontmatter missing title: %q", fm)
	}
	if string(body) != "body text\n" {
		t.Errorf("body = %q want %q", body, "body text\n")
	}
}

func TestParseFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-task.md")
	created := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	original := Issue{
		Slug:       "test-task",
		Title:      "Hello world",
		Priority:   PriorityP1,
		State:      StateOpen,
		Created:    created,
		Acceptance: []string{"item 1", "item 2"},
		Body:       "Some details about the task.",
	}
	if err := WriteFile(original, path); err != nil {
		t.Fatalf("write: %v", err)
	}
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Slug != original.Slug {
		t.Errorf("slug: %q != %q", parsed.Slug, original.Slug)
	}
	if parsed.Title != original.Title {
		t.Errorf("title: %q != %q", parsed.Title, original.Title)
	}
	if parsed.Priority != original.Priority {
		t.Errorf("priority: %q != %q", parsed.Priority, original.Priority)
	}
	if !parsed.Created.Equal(original.Created) {
		t.Errorf("created: %v != %v", parsed.Created, original.Created)
	}
	if len(parsed.Acceptance) != 2 || parsed.Acceptance[0] != "item 1" {
		t.Errorf("acceptance: %v", parsed.Acceptance)
	}
	if !strings.Contains(parsed.Body, "Some details") {
		t.Errorf("body: %q", parsed.Body)
	}
}

func TestMarkState_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	if err := WriteFile(Issue{
		Slug: "x", Title: "x", Priority: PriorityP2, State: StateOpen,
		Created: time.Now().UTC(),
	}, path); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := MarkState(dir, "x", StateDone); err != nil {
		t.Fatalf("mark: %v", err)
	}
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.State != StateDone {
		t.Errorf("state: %q want %q", parsed.State, StateDone)
	}
	// Idempotent: marking done again must not change anything.
	before, _ := os.ReadFile(path)
	if err := MarkState(dir, "x", StateDone); err != nil {
		t.Fatalf("mark again: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("non-idempotent: before=%q after=%q", before, after)
	}
}

func TestSetGiteaIssue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	if err := WriteFile(Issue{
		Slug: "x", Title: "x", Priority: PriorityP2, State: StateOpen,
		Created: time.Now().UTC(),
	}, path); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SetGiteaIssue(dir, "x", 42); err != nil {
		t.Fatalf("set: %v", err)
	}
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.GiteaIssue == nil || *parsed.GiteaIssue != 42 {
		t.Errorf("gitea_issue: %v want 42", parsed.GiteaIssue)
	}
}

func TestSlugMarker_RoundTrip(t *testing.T) {
	issue := Issue{
		Slug:       "cnl-executor",
		Title:      "X",
		Body:       "Some content.",
		Acceptance: []string{"foo"},
	}
	body := RenderGiteaBody(issue)
	if !strings.HasPrefix(body, SlugMarkerPrefix+"cnl-executor"+SlugMarkerSuffix) {
		t.Errorf("body missing marker: %q", body)
	}
	got := SlugFromGiteaBody(body)
	if got != "cnl-executor" {
		t.Errorf("slug from body: %q want %q", got, "cnl-executor")
	}
}

func TestPriorityRank(t *testing.T) {
	if priorityRank(PriorityP1) >= priorityRank(PriorityP2) {
		t.Error("p1 should outrank p2")
	}
	if priorityRank(PriorityP2) >= priorityRank(PriorityP3) {
		t.Error("p2 should outrank p3")
	}
}

func TestLoad_SortsByPriorityThenAge(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustWrite(t, dir, Issue{Slug: "old-p2", Title: "old p2", Priority: PriorityP2, State: StateOpen, Created: t0})
	mustWrite(t, dir, Issue{Slug: "new-p1", Title: "new p1", Priority: PriorityP1, State: StateOpen, Created: t0.Add(24 * time.Hour)})
	mustWrite(t, dir, Issue{Slug: "old-p1", Title: "old p1", Priority: PriorityP1, State: StateOpen, Created: t0})
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len: %d", len(got))
	}
	want := []string{"old-p1", "new-p1", "old-p2"}
	for i, w := range want {
		if got[i].Slug != w {
			t.Errorf("[%d] %q want %q", i, got[i].Slug, w)
		}
	}
}

func TestLoad_SkipsReadmeAndPause(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "real", Title: "real", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Backlog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PAUSE"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "real" {
		t.Errorf("got %v", got)
	}
	if !PauseSentinelExists(dir) {
		t.Error("PAUSE sentinel not detected")
	}
}

func TestReconcile_CreatesNewIssues(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "first", Title: "First task", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	mustWrite(t, dir, Issue{Slug: "second", Title: "Second task", Priority: PriorityP2, State: StateOpen, Created: time.Now().UTC()})

	p := &projects.Project{Slug: "test-repo"}
	fg := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(fg.srv.URL, "test-token")

	if err := Reconcile(context.Background(), dir, c, p, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(fg.issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(fg.issues))
	}
	// gitea_issue should be back-populated in the markdown.
	parsed, _ := ParseFile(filepath.Join(dir, "first.md"))
	if parsed.GiteaIssue == nil {
		t.Error("first.md missing gitea_issue after reconcile")
	}
	// Body should contain the slug marker.
	if !strings.Contains(fg.issues[0].Body, SlugMarkerPrefix+"first") {
		t.Errorf("issue body missing slug marker: %q", fg.issues[0].Body)
	}
}

func TestReconcile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "a", Title: "A", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	p := &projects.Project{Slug: "test-repo"}
	fg := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(fg.srv.URL, "test-token")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := 0; i < 3; i++ {
		if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if len(fg.issues) != 1 {
		t.Errorf("expected 1 issue after 3 reconciles, got %d", len(fg.issues))
	}
}

func TestReconcile_TitleDriftPropagates(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "a", Title: "Original", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	p := &projects.Project{Slug: "test-repo"}
	fg := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(fg.srv.URL, "test-token")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	// Edit the markdown title.
	parsed, _ := ParseFile(filepath.Join(dir, "a.md"))
	parsed.Title = "Updated"
	if err := WriteFile(parsed, parsed.Path); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if fg.issues[0].Title != "Updated" {
		t.Errorf("Gitea title: %q want %q", fg.issues[0].Title, "Updated")
	}
}

func TestReconcile_GiteaCloseMirrorsToMarkdown(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "a", Title: "A", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	p := &projects.Project{Slug: "test-repo"}
	fg := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(fg.srv.URL, "test-token")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	// Worker closes the issue out-of-band.
	fg.issues[0].State = "closed"
	if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	parsed, _ := ParseFile(filepath.Join(dir, "a.md"))
	if parsed.State != StateDone {
		t.Errorf("state: %q want done", parsed.State)
	}
}

func TestReconcile_RelinksAfterGiteaWipe(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, Issue{Slug: "a", Title: "A", Priority: PriorityP1, State: StateOpen, Created: time.Now().UTC()})
	p := &projects.Project{Slug: "test-repo"}
	fg := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(fg.srv.URL, "test-token")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Reconcile(context.Background(), dir, c, p, log); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	originalNum := *mustParse(t, filepath.Join(dir, "a.md")).GiteaIssue

	// Simulate Gitea wipe + reinit: stop the old fake, swap in a fresh
	// one at the same URL is non-trivial, so instead rotate the client.
	fg2 := newFakeGitea(t, projects.Owner, p.Slug)
	c2 := gitserver.NewClient(fg2.srv.URL, "test-token")

	// On fresh Gitea, the markdown's stored gitea_issue is stale; reconcile
	// should NOT find it by number, fall back to slug match (also fails since
	// fresh Gitea is empty), and create a new issue, writing back the new num.
	if err := Reconcile(context.Background(), dir, c2, p, log); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if len(fg2.issues) != 1 {
		t.Fatalf("expected 1 issue in fresh Gitea, got %d", len(fg2.issues))
	}
	parsed := mustParse(t, filepath.Join(dir, "a.md"))
	if parsed.GiteaIssue == nil {
		t.Error("missing gitea_issue after recreation")
	}
	if *parsed.GiteaIssue == originalNum && fg2.issues[0].Number != originalNum {
		t.Errorf("gitea_issue not updated to fresh number: %d", *parsed.GiteaIssue)
	}
}

// --- helpers ---

func mustWrite(t *testing.T, dir string, issue Issue) {
	t.Helper()
	path := filepath.Join(dir, issue.Slug+".md")
	issue.Path = path
	if err := WriteFile(issue, path); err != nil {
		t.Fatal(err)
	}
}

func mustParse(t *testing.T, path string) Issue {
	t.Helper()
	i, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
