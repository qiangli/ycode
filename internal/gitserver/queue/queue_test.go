package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// fakeGitea is a tiny in-memory Gitea-shaped server for queue tests.
type fakeGitea struct {
	mu     *http.ServeMux
	srv    *httptest.Server
	t      *testing.T
	owner  string
	repo   string
	issues []gitserver.Issue
	labels []gitserver.Label
	nextID int64
}

func newFakeGitea(t *testing.T, owner, repo string) *fakeGitea {
	t.Helper()
	f := &fakeGitea{
		t:      t,
		owner:  owner,
		repo:   repo,
		nextID: 1,
	}
	mux := http.NewServeMux()
	f.mu = mux
	f.register(mux)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitea) URL() string { return f.srv.URL }

func (f *fakeGitea) register(mux *http.ServeMux) {
	prefix := "/api/v1/repos/" + f.owner + "/" + f.repo

	mux.HandleFunc(prefix+"/labels", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			writeJSON(w, f.labels)
		case "POST":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			l := gitserver.Label{
				ID:    f.nextID,
				Name:  body["name"].(string),
				Color: body["color"].(string),
			}
			f.nextID++
			f.labels = append(f.labels, l)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, l)
		}
	})

	mux.HandleFunc(prefix+"/issues", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			state := r.URL.Query().Get("state")
			out := []gitserver.Issue{}
			for _, i := range f.issues {
				if state == "" || state == "all" || i.State == state {
					out = append(out, i)
				}
			}
			writeJSON(w, out)
		case "POST":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			labelNames, _ := body["labels"].([]any)
			var lbls []gitserver.Label
			for _, n := range labelNames {
				lbls = append(lbls, f.findOrCreateLabel(n.(string)))
			}
			i := gitserver.Issue{
				ID:     f.nextID,
				Number: f.nextID,
				Title:  body["title"].(string),
				State:  "open",
				Labels: lbls,
			}
			if b, ok := body["body"].(string); ok {
				i.Body = b
			}
			f.nextID++
			f.issues = append(f.issues, i)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, i)
		}
	})

	mux.HandleFunc(prefix+"/issues/", func(w http.ResponseWriter, r *http.Request) {
		// PATCH /issues/<n>  -> update labels/state
		// GET   /issues/<n>  -> fetch
		path := strings.TrimPrefix(r.URL.Path, prefix+"/issues/")
		var num int64
		_, _ = fmtSscan(path, &num)
		idx := -1
		for i, iss := range f.issues {
			if iss.Number == num {
				idx = i
				break
			}
		}
		if idx < 0 {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, f.issues[idx])
		case "PATCH":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if labels, ok := body["labels"].([]any); ok {
				var newLabels []gitserver.Label
				byID := make(map[int64]gitserver.Label)
				for _, l := range f.labels {
					byID[l.ID] = l
				}
				for _, lid := range labels {
					var id int64
					switch v := lid.(type) {
					case float64:
						id = int64(v)
					case int64:
						id = v
					}
					if l, ok := byID[id]; ok {
						newLabels = append(newLabels, l)
					}
				}
				f.issues[idx].Labels = newLabels
			}
			if state, ok := body["state"].(string); ok {
				f.issues[idx].State = state
			}
			writeJSON(w, f.issues[idx])
		}
	})
}

func (f *fakeGitea) findOrCreateLabel(name string) gitserver.Label {
	for _, l := range f.labels {
		if l.Name == name {
			return l
		}
	}
	l := gitserver.Label{ID: f.nextID, Name: name}
	f.nextID++
	f.labels = append(f.labels, l)
	return l
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// fmtSscan is a minimal stand-in for fmt.Sscan to avoid the import cycle
// confusion when reading test path parts.
func fmtSscan(s string, p *int64) (int, error) {
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int64(s[i]-'0')
	}
	*p = n
	return 1, nil
}

// --- tests ---

func setupProject(t *testing.T) *projects.Project {
	t.Helper()
	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), t.TempDir())
	return p
}

func TestEnsureLabels_CreatesAll(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")

	if err := EnsureLabels(context.Background(), c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	got := map[string]bool{}
	for _, l := range f.labels {
		got[l.Name] = true
	}
	for _, want := range LabelsToInit() {
		if !got[want] {
			t.Errorf("label %q not created", want)
		}
	}
}

func TestSubmit_AppliesPriority(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	issue, err := Submit(context.Background(), c, p, SubmitOptions{
		Title:    "fix login redirect",
		Priority: LabelP1,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !HasLabel(issue, LabelP1) {
		t.Errorf("expected p1 label, got %+v", issue.Labels)
	}
	if Priority(issue) != LabelP1 {
		t.Errorf("Priority: got %q want p1", Priority(issue))
	}
}

func TestSubmit_DefaultsToP2(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	i, err := Submit(context.Background(), c, p, SubmitOptions{Title: "no priority"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if Priority(i) != LabelP2 {
		t.Errorf("expected default p2, got %q", Priority(i))
	}
}

func TestPop_HighestPriorityFirst(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	_, _ = Submit(context.Background(), c, p, SubmitOptions{Title: "low", Priority: LabelP3})
	want, _ := Submit(context.Background(), c, p, SubmitOptions{Title: "urgent", Priority: LabelP1})
	_, _ = Submit(context.Background(), c, p, SubmitOptions{Title: "mid", Priority: LabelP2})

	got, err := Pop(context.Background(), c, p, "agent-A")
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got == nil || got.Number != want.Number {
		t.Errorf("Pop returned wrong issue: %+v", got)
	}
	if !HasLabel(got, LabelInProgress) {
		t.Errorf("expected in-progress label after Pop")
	}
	if ClaimedBy(got) != "agent-A" {
		t.Errorf("ClaimedBy: got %q want agent-A", ClaimedBy(got))
	}
}

func TestPop_SkipsClaimed(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	first, _ := Submit(context.Background(), c, p, SubmitOptions{Title: "first", Priority: LabelP1})
	second, _ := Submit(context.Background(), c, p, SubmitOptions{Title: "second", Priority: LabelP2})

	if got, err := Pop(context.Background(), c, p, "agent-A"); err != nil || got.Number != first.Number {
		t.Fatalf("Pop A: %v %+v", err, got)
	}
	if got, err := Pop(context.Background(), c, p, "agent-B"); err != nil || got.Number != second.Number {
		t.Fatalf("Pop B: %v %+v", err, got)
	}
}

func TestPop_NilWhenEmpty(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	got, err := Pop(context.Background(), c, p, "agent-A")
	if err != nil {
		t.Fatalf("Pop empty: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestRelease(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	i, _ := Submit(context.Background(), c, p, SubmitOptions{Title: "x", Priority: LabelP1})
	_, _ = Pop(context.Background(), c, p, "agent-A")

	if err := Release(context.Background(), c, p, i.Number, "agent-A"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got, _ := c.GetIssue(context.Background(), projects.Owner, p.Slug, i.Number)
	if HasLabel(got, LabelInProgress) {
		t.Errorf("expected in-progress to be removed")
	}
	if ClaimedBy(got) != "" {
		t.Errorf("expected claim removed, got %q", ClaimedBy(got))
	}
}

func TestComplete(t *testing.T) {
	p := setupProject(t)
	f := newFakeGitea(t, projects.Owner, p.Slug)
	c := gitserver.NewClient(f.URL(), "tok")
	_ = EnsureLabels(context.Background(), c, p)

	i, _ := Submit(context.Background(), c, p, SubmitOptions{Title: "x"})
	if err := Complete(context.Background(), c, p, i.Number); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, _ := c.GetIssue(context.Background(), projects.Owner, p.Slug, i.Number)
	if got.State != "closed" {
		t.Errorf("expected closed, got %q", got.State)
	}
}
