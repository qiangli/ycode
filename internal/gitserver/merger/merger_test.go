package merger

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

func TestIssueFromHeadRef(t *testing.T) {
	cases := []struct {
		ref  string
		want int64
	}{
		{"agent/agent-abc/issue-42", 42},
		{"agent/agent-deadbeef/issue-1", 1},
		{"agent/agent-abc/free-1234", 0},
		{"main", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := issueFromHeadRef(c.ref); got != c.want {
			t.Errorf("issueFromHeadRef(%q) = %d, want %d", c.ref, got, c.want)
		}
	}
}

func TestAgentIDFromHeadRef(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"agent/agent-abcdef12/issue-1", "agent-abcdef12"},
		{"agent/agent-deadbeef/free-x", "agent-deadbeef"},
		{"main", ""},
		{"upstream/main", ""},
	}
	for _, c := range cases {
		if got := agentIDFromHeadRef(c.ref); got != c.want {
			t.Errorf("agentIDFromHeadRef(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestInjectToken(t *testing.T) {
	cases := []struct {
		in    string
		token string
		want  string
	}{
		{"http://127.0.0.1:3000/admin/x.git", "tok", "http://token:tok@127.0.0.1:3000/admin/x.git"},
		{"https://gitea.local/admin/x.git", "tok", "https://token:tok@gitea.local/admin/x.git"},
		{"git@example.com:foo/bar.git", "tok", "git@example.com:foo/bar.git"},
	}
	for _, c := range cases {
		got, err := injectToken(c.in, c.token)
		if err != nil {
			t.Errorf("injectToken(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("injectToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTail(t *testing.T) {
	if got := tail("hello", 100); got != "hello" {
		t.Errorf("short string passthrough: %q", got)
	}
	if got := tail("0123456789", 4); got != "..."+"6789" {
		t.Errorf("tail: %q", got)
	}
}

func TestNew_RequiresFields(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error on empty config")
	}
}

// TestProcessPR_NoCICommand_AutoMerges verifies that when CICommand is "",
// the merger goes straight to MergePR. This exercises the merge code path
// without needing a real git checkout.
func TestProcessPR_NoCICommand_AutoMerges(t *testing.T) {
	r, _ := projects.NewRegistry(t.TempDir())
	p, _ := r.Resolve(context.Background(), t.TempDir())

	mergeCalled := false
	closeCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/pulls") && req.URL.Query().Get("state") == "open":
			// Write raw JSON so we don't have to mirror the anonymous-struct json tags.
			io.WriteString(w, `[{"id":1,"number":1,"state":"open","head":{"label":"","ref":"agent/agent-abc/issue-7"},"base":{"label":"","ref":"main"}}]`)
		case req.Method == "POST" && strings.Contains(req.URL.Path, "/pulls/1/merge"):
			mergeCalled = true
			w.WriteHeader(http.StatusOK)
		case req.Method == "GET" && strings.Contains(req.URL.Path, "/issues/7"):
			writeJSON(w, gitserver.Issue{Number: 7, State: "open"})
		case req.Method == "PATCH" && strings.Contains(req.URL.Path, "/issues/7"):
			closeCalled = true
			var body map[string]any
			json.NewDecoder(req.Body).Decode(&body)
			if body["state"] != "closed" {
				t.Errorf("expected state=closed, got %v", body["state"])
			}
			writeJSON(w, gitserver.Issue{Number: 7, State: "closed"})
		case req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/labels"):
			writeJSON(w, []gitserver.Label{})
		default:
			io.Copy(io.Discard, req.Body)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
		}
	}))
	defer srv.Close()

	c := gitserver.NewClient(srv.URL, "tok")
	syncLog, err := projects.NewSyncLog(t.TempDir(), p)
	if err != nil {
		t.Fatalf("NewSyncLog: %v", err)
	}

	m, err := New(Config{
		Client:    c,
		Project:   p,
		SyncLog:   syncLog,
		CloneURL:  "http://127.0.0.1:1/admin/" + p.Slug + ".git", // unreachable; OK because we skip SHA fetch path
		Token:     "tok",
		WorkDir:   t.TempDir(),
		CICommand: "",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !mergeCalled {
		t.Errorf("expected MergePR to be called")
	}
	if !closeCalled {
		t.Errorf("expected issue 7 to be closed")
	}

	// pending-sync log should have one entry.
	entries, err := syncLog.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 sync entry, got %d", len(entries))
	}
	if entries[0].PR != 1 {
		t.Errorf("PR: got %d want 1", entries[0].PR)
	}
	if entries[0].AgentID != "agent-abc" {
		t.Errorf("AgentID: got %q want agent-abc", entries[0].AgentID)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
