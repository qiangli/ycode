// Package contract provides deterministic validation tests for ycode's
// infrastructure. These tests run without LLM, network, or containers.
package contract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/github"
	"github.com/qiangli/ycode/internal/runtime/treesitter"
)

// =============================================================================
// Tree-sitter: Validate in-process AST parsing across languages
// =============================================================================

func TestTreeSitter_ParseGoExtractsSymbols(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tree-sitter test in short mode")
	}

	source := []byte(`package main

import "fmt"

type Server struct {
	Port int
}

type Handler interface {
	ServeHTTP()
}

func NewServer(port int) *Server {
	return &Server{Port: port}
}

func (s *Server) Start() error {
	fmt.Println("starting")
	return nil
}

const Version = "1.0"
`)

	parser := treesitter.NewParser()
	tree, err := parser.Parse(context.Background(), source, "go")
	if err != nil {
		t.Fatalf("Parse Go: %v", err)
	}

	symbols := treesitter.ExtractSymbols(tree, "server.go")

	// Verify expected symbols are found.
	found := make(map[string]string) // name -> kind
	for _, s := range symbols {
		found[s.Name] = s.Kind
	}

	expected := map[string]string{
		"Server":    "type",
		"Handler":   "interface",
		"NewServer": "func",
		"Start":     "method",
	}

	for name, kind := range expected {
		if found[name] != kind {
			t.Errorf("expected %s %s, got %q", kind, name, found[name])
		}
	}

	// Verify exported detection.
	for _, s := range symbols {
		if s.Name == "Server" && !s.Exported {
			t.Error("Server should be exported")
		}
	}
}

func TestTreeSitter_ParsePythonExtractsSymbols(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tree-sitter test in short mode")
	}

	source := []byte(`
class UserService:
    def __init__(self, db):
        self.db = db

    def get_user(self, id):
        return self.db.find(id)

def create_app():
    return UserService(None)

def _internal_helper():
    pass
`)

	parser := treesitter.NewParser()
	tree, err := parser.Parse(context.Background(), source, "python")
	if err != nil {
		t.Fatalf("Parse Python: %v", err)
	}

	symbols := treesitter.ExtractSymbols(tree, "service.py")

	found := make(map[string]bool)
	for _, s := range symbols {
		found[s.Name] = true
		// _internal_helper should not be exported.
		if s.Name == "_internal_helper" && s.Exported {
			t.Error("_internal_helper should not be exported")
		}
	}

	if !found["UserService"] {
		t.Error("expected to find class UserService")
	}
	if !found["create_app"] {
		t.Error("expected to find function create_app")
	}
}

func TestTreeSitter_AllLanguagesSupported(t *testing.T) {
	langs := treesitter.SupportedLanguages()
	required := []string{"go", "python", "javascript", "typescript", "rust", "java", "c", "ruby"}

	supported := make(map[string]bool)
	for _, l := range langs {
		supported[l] = true
	}

	for _, lang := range required {
		if !supported[lang] {
			t.Errorf("expected language %q to be supported", lang)
		}
	}
}

func TestTreeSitter_AliasesResolve(t *testing.T) {
	aliases := map[string]bool{
		"py": true, "js": true, "ts": true, "rs": true, "rb": true,
	}
	for alias := range aliases {
		if !treesitter.IsSupported(alias) {
			t.Errorf("alias %q should resolve to a supported language", alias)
		}
	}
}

func TestTreeSitter_SearchTextFindsPatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tree-sitter test in short mode")
	}

	source := []byte(`package main

func handleError(err error) {
	if err != nil {
		panic(err)
	}
}

func processData(data []byte) error {
	return nil
}
`)

	parser := treesitter.NewParser()
	matches, err := treesitter.SearchText(context.Background(), parser, source, "go", "func handleError", "main.go")
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(matches) == 0 {
		t.Error("expected at least one match for 'func handleError'")
	}
}

func TestTreeSitter_ParseRealGoFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping codebase test in short mode")
	}

	root := repoRoot(t)
	path := filepath.Join(root, "internal", "runtime", "treesitter", "parser.go")

	source, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read parser.go: %v", err)
	}

	parser := treesitter.NewParser()
	tree, err := parser.Parse(context.Background(), source, "go")
	if err != nil {
		t.Fatalf("Parse own source: %v", err)
	}

	symbols := treesitter.ExtractSymbols(tree, "parser.go")
	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols from parser.go, got %d", len(symbols))
	}

	// Should find NewParser and Parse.
	found := make(map[string]bool)
	for _, s := range symbols {
		found[s.Name] = true
	}
	if !found["NewParser"] {
		t.Error("expected to find NewParser in parser.go")
	}
}

// =============================================================================
// GitHub: Validate remote URL parsing and type construction
// =============================================================================

func TestGitHub_ParseRemoteURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https://github.com/qiangli/ycode.git", "qiangli", "ycode", false},
		{"https://github.com/qiangli/ycode", "qiangli", "ycode", false},
		{"git@github.com:qiangli/ycode.git", "qiangli", "ycode", false},
		{"git@github.com:qiangli/ycode", "qiangli", "ycode", false},
		{"https://github.com/org-name/repo-name.git", "org-name", "repo-name", false},
		{"https://gitlab.com/owner/repo.git", "", "", true},
		{"", "", "", true},
	}

	for _, tc := range tests {
		owner, repo, err := github.ParseRemoteURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseRemoteURL(%q): err=%v, wantErr=%v", tc.url, err, tc.wantErr)
			continue
		}
		if owner != tc.wantOwner || repo != tc.wantRepo {
			t.Errorf("ParseRemoteURL(%q) = (%q, %q), want (%q, %q)",
				tc.url, owner, repo, tc.wantOwner, tc.wantRepo)
		}
	}
}

func TestGitHub_DetectRepoFromCurrentDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git-dependent test in short mode")
	}

	root := repoRoot(t)
	owner, repo, err := github.DetectRepo(root)
	if err != nil {
		t.Skipf("cannot detect repo (not a GitHub remote?): %v", err)
	}

	// We know ycode is on GitHub.
	if owner == "" || repo == "" {
		t.Errorf("expected non-empty owner/repo, got (%q, %q)", owner, repo)
	}

	if !strings.Contains(repo, "ycode") {
		t.Errorf("expected repo to contain 'ycode', got %q", repo)
	}
}

func TestGitHub_FormatPR(t *testing.T) {
	pr := &github.PR{
		Number:  42,
		Title:   "Add tree-sitter support",
		State:   "open",
		Author:  "user",
		HeadRef: "feature/treesitter",
		BaseRef: "main",
		URL:     "https://github.com/org/repo/pull/42",
		Body:    "This PR adds tree-sitter parsing.",
	}

	formatted := github.FormatPR(pr)
	if !strings.Contains(formatted, "#42") {
		t.Error("expected PR number in formatted output")
	}
	if !strings.Contains(formatted, "Add tree-sitter support") {
		t.Error("expected title in formatted output")
	}
	if !strings.Contains(formatted, "feature/treesitter") {
		t.Error("expected branch in formatted output")
	}
}

func TestGitHub_FormatIssue(t *testing.T) {
	issue := &github.Issue{
		Number: 100,
		Title:  "Bug: crash on startup",
		State:  "open",
		Author: "reporter",
		Labels: []string{"bug", "critical"},
		URL:    "https://github.com/org/repo/issues/100",
		Body:   "ycode crashes when...",
	}

	formatted := github.FormatIssue(issue)
	if !strings.Contains(formatted, "#100") {
		t.Error("expected issue number in formatted output")
	}
	if !strings.Contains(formatted, "bug, critical") {
		t.Error("expected labels in formatted output")
	}
}
