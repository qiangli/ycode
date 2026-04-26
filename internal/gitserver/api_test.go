package gitserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:3000", "test-token")
	if c.baseURL != "http://localhost:3000" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
	if c.token != "test-token" {
		t.Errorf("unexpected token: %s", c.token)
	}
	if c.http == nil {
		t.Error("http client is nil")
	}
}

func TestClientListRepos(t *testing.T) {
	// Mock Gitea API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/repos" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}

		repos := []Repository{
			{ID: 1, Name: "repo1", FullName: "admin/repo1"},
			{ID: 2, Name: "repo2", FullName: "admin/repo2"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].Name != "repo1" {
		t.Errorf("unexpected name: %s", repos[0].Name)
	}
	if repos[1].FullName != "admin/repo2" {
		t.Errorf("unexpected full name: %s", repos[1].FullName)
	}
}

func TestClientCreateRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/repos" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "new-repo" {
			t.Errorf("unexpected repo name: %v", body["name"])
		}
		if body["default_branch"] != "main" {
			t.Errorf("unexpected default branch: %v", body["default_branch"])
		}

		repo := Repository{
			ID:       1,
			Name:     "new-repo",
			FullName: "admin/new-repo",
			CloneURL: "http://localhost:3000/admin/new-repo.git",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(repo)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	repo, err := c.CreateRepo(context.Background(), "new-repo", "test description")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.Name != "new-repo" {
		t.Errorf("unexpected name: %s", repo.Name)
	}
}

func TestClientCreateBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/admin/repo1/branches" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["new_branch_name"] != "agent/test-1" {
			t.Errorf("unexpected branch name: %v", body["new_branch_name"])
		}
		if body["old_branch_name"] != "main" {
			t.Errorf("unexpected from ref: %v", body["old_branch_name"])
		}

		branch := Branch{Name: "agent/test-1"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(branch)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	branch, err := c.CreateBranch(context.Background(), "admin", "repo1", "agent/test-1", "main")
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if branch.Name != "agent/test-1" {
		t.Errorf("unexpected branch: %s", branch.Name)
	}
}

func TestClientCreatePR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/admin/repo1/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "Agent work: feature X" {
			t.Errorf("unexpected title: %v", body["title"])
		}

		pr := PullRequest{
			ID:      1,
			Number:  42,
			Title:   "Agent work: feature X",
			State:   "open",
			HTMLURL: "http://localhost:3000/admin/repo1/pulls/42",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	pr, err := c.CreatePR(context.Background(), "admin", "repo1", "Agent work: feature X", "agent/test-1", "main")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("unexpected PR number: %d", pr.Number)
	}
	if pr.State != "open" {
		t.Errorf("unexpected state: %s", pr.State)
	}
}

func TestClientListPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("unexpected state filter: %s", r.URL.Query().Get("state"))
		}
		prs := []PullRequest{
			{ID: 1, Number: 1, Title: "PR 1", State: "open"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(prs)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	prs, err := c.ListPRs(context.Background(), "admin", "repo1", "open")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
}

func TestClientMergePR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/admin/repo1/pulls/1/merge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["Do"] != "merge" {
			t.Errorf("unexpected merge method: %v", body["Do"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	err := c.MergePR(context.Background(), "admin", "repo1", 1, "")
	if err != nil {
		t.Fatalf("MergePR: %v", err)
	}
}

func TestClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	_, err := c.ListRepos(context.Background())
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !stringContains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestClientNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("should not send Authorization header when token is empty")
		}
		json.NewEncoder(w).Encode([]Repository{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
