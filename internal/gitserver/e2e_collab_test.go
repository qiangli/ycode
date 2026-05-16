package gitserver_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/agents"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// TestE2E_MultiAgentCollab is the end-to-end cross-package integration test
// for docs/agent-collab.md. It exercises projects → queue → agents → merger
// composed together against a Gitea-shaped HTTP mock.
//
// Why a mock instead of real embedded Gitea: the embedded server starts but
// has no admin user/token provisioned out of the box (Gitea installer is
// locked by ServerConfig.HTTPOnly). Provisioning that requires running
// the install flow programmatically — separate work, not part of this PR.
// The mock validates every wire interaction this codebase makes against
// Gitea; api_test.go already covers the encoding details for each method.
//
// Workflow exercised:
//  1. Resolve a project (registry creates entry).
//  2. EnsureRepo (creates admin/<slug>).
//  3. EnsureLabels + Submit a task.
//  4. Pop the task as agent-A — claim labels applied.
//  5. AssignBranch — agent gets agent/agent-A/issue-1.
//  6. OpenPR.
//  7. merger.Tick (CICommand="" → unconditional auto-merge).
//  8. Assert: PR closed, issue closed, sync log appended with the
//     correct PR number and agent ID.
func TestE2E_MultiAgentCollab(t *testing.T) {
	ctx := context.Background()
	gitea := newCollabFakeGitea(t)
	c := gitserver.NewClient(gitea.URL(), "tok")

	// 1. Resolve project.
	dataDir := t.TempDir()
	r, err := projects.NewRegistry(dataDir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	p, err := r.Resolve(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// 2. EnsureRepo.
	if _, err := projects.EnsureRepo(ctx, c, p); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	gitea.assertRepoExists(t, p.Slug)

	// 3. EnsureLabels + Submit.
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	issue, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
		Title:    "Add greeting.txt",
		Body:     "Create greeting.txt with 'hello world'",
		Priority: queue.LabelP1,
		Labels:   []string{queue.LabelAutoMerge},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !queue.HasLabel(issue, queue.LabelP1) {
		t.Fatalf("expected p1 label on submitted issue")
	}

	// 4. Pop as agent-A.
	popped, err := queue.Pop(ctx, c, p, "agent-A")
	if err != nil || popped == nil {
		t.Fatalf("Pop: %v %v", err, popped)
	}
	if popped.Number != issue.Number {
		t.Fatalf("Pop returned wrong issue: %d", popped.Number)
	}
	if !queue.HasLabel(popped, queue.LabelInProgress) {
		t.Fatalf("expected in-progress label")
	}
	if queue.ClaimedBy(popped) != "agent-A" {
		t.Fatalf("expected claim by agent-A, got %q", queue.ClaimedBy(popped))
	}
	// A second agent should pop nothing (only one issue, claimed).
	if got, _ := queue.Pop(ctx, c, p, "agent-B"); got != nil {
		t.Errorf("second Pop should be nil, got #%d", got.Number)
	}

	// 5. AssignBranch.
	a := &agents.Agent{ID: "agent-A", Name: "alice"}
	br, err := agents.AssignBranch(ctx, c, p, a, popped.Number)
	if err != nil {
		t.Fatalf("AssignBranch: %v", err)
	}
	wantBranch := "agent/agent-A/issue-" + intToStr(popped.Number)
	if br.Name != wantBranch {
		t.Fatalf("AssignBranch name: got %q want %q", br.Name, wantBranch)
	}

	// 6. OpenPR.
	pr, err := br.OpenPR(ctx, c, "", "Closes #1")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if pr.State != "open" {
		t.Fatalf("expected PR open, got %q", pr.State)
	}
	if pr.Head.Ref != br.Name {
		t.Fatalf("PR head: got %q want %q", pr.Head.Ref, br.Name)
	}
	if pr.Base.Ref != "main" {
		t.Fatalf("PR base: got %q want main", pr.Base.Ref)
	}

	// 7. Merger.Tick — no CI command, auto-merge unconditionally.
	syncLog, err := projects.NewSyncLog(dataDir, p)
	if err != nil {
		t.Fatalf("NewSyncLog: %v", err)
	}
	m, err := merger.New(merger.Config{
		Client:   c,
		Project:  p,
		SyncLog:  syncLog,
		CloneURL: "http://127.0.0.1:1/admin/" + p.Slug + ".git",
		Token:    "tok",
		WorkDir:  t.TempDir(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("merger.New: %v", err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("merger.Tick: %v", err)
	}

	// 8. Assertions.
	mergedPR := gitea.getPR(pr.Number)
	if mergedPR == nil || mergedPR.State != "closed" {
		t.Errorf("expected PR closed after merge, got %+v", mergedPR)
	}
	closedIssue, err := c.GetIssue(ctx, projects.Owner, p.Slug, popped.Number)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if closedIssue.State != "closed" {
		t.Errorf("expected issue closed after merge, got state=%q", closedIssue.State)
	}
	pending, err := syncLog.Pending()
	if err != nil {
		t.Fatalf("synclog.Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending sync entry, got %d", len(pending))
	}
	if pending[0].PR != pr.Number {
		t.Errorf("synclog PR: got %d want %d", pending[0].PR, pr.Number)
	}
	if pending[0].AgentID != "agent-A" {
		t.Errorf("synclog AgentID: got %q want agent-A", pending[0].AgentID)
	}
}

// TestE2E_PriorityOrdering — multiple tasks, two agents pop in priority order.
func TestE2E_PriorityOrdering(t *testing.T) {
	ctx := context.Background()
	gitea := newCollabFakeGitea(t)
	c := gitserver.NewClient(gitea.URL(), "tok")

	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(ctx, t.TempDir())
	_, _ = projects.EnsureRepo(ctx, c, p)
	_ = queue.EnsureLabels(ctx, c, p)

	// File three issues out of priority order.
	_, _ = queue.Submit(ctx, c, p, queue.SubmitOptions{Title: "low", Priority: queue.LabelP3})
	high, _ := queue.Submit(ctx, c, p, queue.SubmitOptions{Title: "urgent", Priority: queue.LabelP1})
	mid, _ := queue.Submit(ctx, c, p, queue.SubmitOptions{Title: "mid", Priority: queue.LabelP2})

	got1, _ := queue.Pop(ctx, c, p, "agent-1")
	if got1 == nil || got1.Number != high.Number {
		t.Fatalf("first pop should be urgent (p1), got %+v", got1)
	}
	got2, _ := queue.Pop(ctx, c, p, "agent-2")
	if got2 == nil || got2.Number != mid.Number {
		t.Fatalf("second pop should be mid (p2), got %+v", got2)
	}
}

// TestE2E_PushOriginRespected — issue with push:origin label triggers OriginPushFn after merge.
func TestE2E_PushOriginRespected(t *testing.T) {
	ctx := context.Background()
	gitea := newCollabFakeGitea(t)
	c := gitserver.NewClient(gitea.URL(), "tok")

	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(ctx, t.TempDir())
	_, _ = projects.EnsureRepo(ctx, c, p)
	_ = queue.EnsureLabels(ctx, c, p)

	// Pre-create an open PR pointing at issue 1, with the push:origin label set on issue 1.
	issue, _ := queue.Submit(ctx, c, p, queue.SubmitOptions{
		Title:    "publish",
		Priority: queue.LabelP1,
		Labels:   []string{queue.LabelPushOrigin},
	})
	gitea.preparePR(1, "agent/agent-A/issue-"+intToStr(issue.Number), "main")

	// Override SHA to a deterministic value (the merger normally fetches it
	// from the post-merge worktree; in the mock there's no real git).
	gitea.postMergeSHA = "deadbeef" + strings.Repeat("0", 32)

	pushed := false
	syncLog, _ := projects.NewSyncLog(t.TempDir(), p)
	m, _ := merger.New(merger.Config{
		Client:   c,
		Project:  p,
		SyncLog:  syncLog,
		CloneURL: "http://127.0.0.1:1/admin/" + p.Slug + ".git",
		Token:    "tok",
		WorkDir:  t.TempDir(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		OriginPushFn: func(ctx context.Context, sha string) error {
			pushed = true
			if sha == "" {
				t.Errorf("expected non-empty SHA passed to OriginPushFn")
			}
			return nil
		},
		FetchMainSHAFn: func(ctx context.Context, prNumber int64) (string, error) {
			return gitea.postMergeSHA, nil
		},
	})
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !pushed {
		t.Errorf("expected OriginPushFn to be invoked for push:origin issue")
	}
}

// --- collabFakeGitea: a Gitea-shaped HTTP mock used by the E2E tests. ---

type collabFakeGitea struct {
	srv          *httptest.Server
	mu           sync.Mutex
	repos        []gitserver.Repository
	branches     map[string][]gitserver.Branch // key: owner/name
	issues       []gitserver.Issue
	prs          []gitserver.PullRequest
	labels       map[string][]gitserver.Label // key: owner/name
	nextID       int64
	postMergeSHA string // injected by tests to override "git rev-parse" path

	// Counters for assertions.
	mergeCallsByPR map[int64]int
}

func newCollabFakeGitea(t *testing.T) *collabFakeGitea {
	t.Helper()
	f := &collabFakeGitea{
		branches:       map[string][]gitserver.Branch{},
		labels:         map[string][]gitserver.Label{},
		mergeCallsByPR: map[int64]int{},
		nextID:         1,
	}
	mux := http.NewServeMux()
	f.register(mux)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *collabFakeGitea) URL() string { return f.srv.URL }

func (f *collabFakeGitea) preparePR(num int64, head, base string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr := gitserver.PullRequest{ID: num, Number: num, State: "open"}
	pr.Head.Ref = head
	pr.Base.Ref = base
	f.prs = append(f.prs, pr)
}

func (f *collabFakeGitea) getPR(num int64) *gitserver.PullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.prs {
		if f.prs[i].Number == num {
			pr := f.prs[i]
			return &pr
		}
	}
	return nil
}

func (f *collabFakeGitea) assertRepoExists(t *testing.T, name string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.repos {
		if r.Name == name {
			return
		}
	}
	t.Fatalf("repo %q not created", name)
}

func (f *collabFakeGitea) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/user/repos", f.handleRepos)
	mux.HandleFunc("/api/v1/repos/", f.handleRepoSubpath)
}

func (f *collabFakeGitea) handleRepos(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case "GET":
		writeJSON(w, f.repos)
	case "POST":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body["name"].(string)
		repo := gitserver.Repository{
			ID:       f.nextID,
			Name:     name,
			FullName: projects.Owner + "/" + name,
			CloneURL: f.srv.URL + "/" + projects.Owner + "/" + name + ".git",
		}
		f.nextID++
		f.repos = append(f.repos, repo)
		f.branches[projects.Owner+"/"+name] = []gitserver.Branch{{Name: "main"}}
		f.labels[projects.Owner+"/"+name] = nil
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, repo)
	}
}

func (f *collabFakeGitea) handleRepoSubpath(w http.ResponseWriter, r *http.Request) {
	// /api/v1/repos/{owner}/{name}/{thing}[/{id}[/{action}]]
	prefix := "/api/v1/repos/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}
	owner, name, thing := parts[0], parts[1], parts[2]
	repoKey := owner + "/" + name

	f.mu.Lock()
	defer f.mu.Unlock()

	switch thing {
	case "labels":
		switch r.Method {
		case "GET":
			writeJSON(w, f.labels[repoKey])
		case "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			lbl := gitserver.Label{
				ID:    f.nextID,
				Name:  body["name"].(string),
				Color: body["color"].(string),
			}
			f.nextID++
			f.labels[repoKey] = append(f.labels[repoKey], lbl)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, lbl)
		}
	case "branches":
		switch r.Method {
		case "GET":
			writeJSON(w, f.branches[repoKey])
		case "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bname, _ := body["new_branch_name"].(string)
			for _, b := range f.branches[repoKey] {
				if b.Name == bname {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"message":"branch already exists"}`))
					return
				}
			}
			br := gitserver.Branch{Name: bname}
			f.branches[repoKey] = append(f.branches[repoKey], br)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, br)
		}
	case "issues":
		f.handleIssues(w, r, repoKey, parts)
	case "pulls":
		f.handlePulls(w, r, repoKey, parts)
	default:
		http.NotFound(w, r)
	}
}

func (f *collabFakeGitea) handleIssues(w http.ResponseWriter, r *http.Request, repoKey string, parts []string) {
	if len(parts) == 3 {
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
			_ = json.NewDecoder(r.Body).Decode(&body)
			labelNames, _ := body["labels"].([]any)
			var lbls []gitserver.Label
			for _, n := range labelNames {
				lbls = append(lbls, f.findOrCreateLabelLocked(repoKey, n.(string)))
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
		return
	}
	// /repos/.../issues/{n}
	num, err := parseInt(parts[3])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	idx := -1
	for i := range f.issues {
		if f.issues[i].Number == num {
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
		_ = json.NewDecoder(r.Body).Decode(&body)
		if labels, ok := body["labels"].([]any); ok {
			byID := map[int64]gitserver.Label{}
			for _, l := range f.labels[repoKey] {
				byID[l.ID] = l
			}
			var newLabels []gitserver.Label
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
}

func (f *collabFakeGitea) handlePulls(w http.ResponseWriter, r *http.Request, repoKey string, parts []string) {
	_ = repoKey
	if len(parts) == 3 {
		switch r.Method {
		case "GET":
			state := r.URL.Query().Get("state")
			out := []gitserver.PullRequest{}
			for _, pr := range f.prs {
				if state == "" || state == "all" || pr.State == state {
					out = append(out, pr)
				}
			}
			writeJSON(w, out)
		case "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			pr := gitserver.PullRequest{
				ID:     f.nextID,
				Number: f.nextID,
				Title:  body["title"].(string),
				State:  "open",
			}
			pr.Head.Ref = body["head"].(string)
			pr.Base.Ref = body["base"].(string)
			f.nextID++
			f.prs = append(f.prs, pr)
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, pr)
		}
		return
	}
	// /pulls/{n}/merge
	if len(parts) >= 5 && parts[4] == "merge" {
		num, err := parseInt(parts[3])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f.mergeCallsByPR[num]++
		for i := range f.prs {
			if f.prs[i].Number == num {
				f.prs[i].State = "closed"
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (f *collabFakeGitea) findOrCreateLabelLocked(repoKey, name string) gitserver.Label {
	for _, l := range f.labels[repoKey] {
		if l.Name == name {
			return l
		}
	}
	l := gitserver.Label{ID: f.nextID, Name: name, Color: "ededed"}
	f.nextID++
	f.labels[repoKey] = append(f.labels[repoKey], l)
	return l
}

// --- small primitives ---

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func parseInt(s string) (int64, error) {
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n, nil
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n, nil
}

func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
