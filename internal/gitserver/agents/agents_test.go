package agents

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

func TestNewAgent_HasStableID(t *testing.T) {
	a := NewAgent("alice")
	if !strings.HasPrefix(a.ID, "agent-") {
		t.Errorf("expected agent- prefix, got %s", a.ID)
	}
	if a.Name != "alice" {
		t.Errorf("expected name=alice, got %s", a.Name)
	}
	b := NewAgent("alice")
	if a.ID == b.ID {
		t.Error("expected distinct IDs across NewAgent calls")
	}
}

func TestAuthorTrailer(t *testing.T) {
	a := &Agent{ID: "agent-deadbeef"}
	want := "agent-deadbeef <agent-deadbeef@ycode.local>"
	if got := a.AuthorTrailer(); got != want {
		t.Errorf("AuthorTrailer: got %q want %q", got, want)
	}
}

func TestBranchName_WithIssue(t *testing.T) {
	a := &Agent{ID: "agent-abc"}
	if got := BranchName(a, 42); got != "agent/agent-abc/issue-42" {
		t.Errorf("BranchName: %q", got)
	}
}

func TestBranchName_FreeForm(t *testing.T) {
	a := &Agent{ID: "agent-abc"}
	got := BranchName(a, 0)
	if !strings.HasPrefix(got, "agent/agent-abc/free-") {
		t.Errorf("expected free-form prefix, got %q", got)
	}
}

func TestAssignBranch_Idempotent(t *testing.T) {
	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), t.TempDir())

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		// Simulate "already exists" on second call.
		if calls > 1 {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"message":"branch already exists"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(gitserver.Branch{Name: "agent/agent-x/issue-1"})
	}))
	defer srv.Close()

	c := gitserver.NewClient(srv.URL, "tok")
	a := &Agent{ID: "agent-x"}

	br1, err := AssignBranch(context.Background(), c, p, a, 1)
	if err != nil {
		t.Fatalf("first AssignBranch: %v", err)
	}
	if br1.Name != "agent/agent-x/issue-1" {
		t.Errorf("unexpected branch name: %s", br1.Name)
	}
	br2, err := AssignBranch(context.Background(), c, p, a, 1)
	if err != nil {
		t.Fatalf("second AssignBranch should be idempotent: %v", err)
	}
	if br2.Name != br1.Name {
		t.Errorf("idempotent assign produced different name: %s vs %s", br2.Name, br1.Name)
	}
}

func TestOpenPR_DefaultTitle(t *testing.T) {
	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), t.TempDir())

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		json.NewDecoder(req.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(gitserver.PullRequest{Number: 1, State: "open"})
	}))
	defer srv.Close()

	c := gitserver.NewClient(srv.URL, "tok")
	a := &Agent{ID: "agent-y"}
	br := &Branch{Project: p, Agent: a, Name: BranchName(a, 5), IssueNo: 5}

	pr, err := br.OpenPR(context.Background(), c, "", "")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if pr.Number != 1 {
		t.Errorf("unexpected PR num: %d", pr.Number)
	}
	if !strings.Contains(captured["title"].(string), "agent-y") {
		t.Errorf("expected agent ID in title, got: %v", captured["title"])
	}
	if captured["head"] != "agent/agent-y/issue-5" {
		t.Errorf("unexpected head: %v", captured["head"])
	}
	if captured["base"] != "main" {
		t.Errorf("unexpected base: %v", captured["base"])
	}
}
