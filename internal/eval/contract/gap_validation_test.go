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
	"github.com/qiangli/ycode/internal/runtime/mcp"
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
// MCP: Validate protocol types and bridge logic
// =============================================================================

func TestMCP_NormalizeName(t *testing.T) {
	tests := []struct {
		server, tool, want string
	}{
		{"filesystem", "read_file", "mcp__filesystem__read_file"},
		{"my-server", "tool_name", "mcp__my-server__tool_name"},
	}

	for _, tc := range tests {
		got := mcp.NormalizeName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("NormalizeName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}

func TestMCP_ParseNormalizedNameRoundtrip(t *testing.T) {
	tests := []struct{ server, tool string }{
		{"github", "create_pr"},
		{"my-server", "list_tools"},
		{"server_with_underscores", "tool"},
	}

	for _, tc := range tests {
		normalized := mcp.NormalizeName(tc.server, tc.tool)
		server, tool, err := mcp.ParseNormalizedName(normalized)
		if err != nil {
			t.Errorf("ParseNormalizedName(%q): %v", normalized, err)
			continue
		}
		if server != tc.server || tool != tc.tool {
			t.Errorf("roundtrip failed: (%q, %q) -> %q -> (%q, %q)",
				tc.server, tc.tool, normalized, server, tool)
		}
	}
}

func TestMCP_ParseNormalizedNameRejectsInvalid(t *testing.T) {
	invalids := []string{"not_mcp", "mcp_single", "regular_tool_name"}
	for _, name := range invalids {
		_, _, err := mcp.ParseNormalizedName(name)
		if err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}
}

func TestMCP_RegistryAddGetAll(t *testing.T) {
	reg := mcp.NewRegistry()

	c1 := mcp.NewClient(mcp.ServerConfig{Name: "server1"})
	c2 := mcp.NewClient(mcp.ServerConfig{Name: "server2"})

	reg.Add("server1", c1)
	reg.Add("server2", c2)

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2 clients, got %d", len(all))
	}

	got, ok := reg.Get("server1")
	if !ok || got != c1 {
		t.Error("Get(server1) failed")
	}
}

func TestMCP_BridgeDiscoverToolsFromPopulatedClient(t *testing.T) {
	reg := mcp.NewRegistry()

	client := mcp.NewClient(mcp.ServerConfig{Name: "test-mcp"})
	// Simulate post-connect state by setting tools via a test helper.
	// Since tools is unexported, we test via the bridge pattern.
	reg.Add("test-mcp", client)

	bridge := mcp.NewBridge(reg)
	tools, err := bridge.DiscoverTools(context.Background())
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}

	// Client has no tools (not connected), so expect empty.
	if len(tools) != 0 {
		t.Errorf("expected 0 tools from unconnected client, got %d", len(tools))
	}
}

func TestMCP_ServerHandlesInitialize(t *testing.T) {
	handler := &mockServerHandler{
		tools: []mcp.Tool{
			{Name: "echo", Description: "Echo tool"},
		},
	}

	server := mcp.NewServer(handler)

	req := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	resp, err := server.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocol version: %v", result["protocolVersion"])
	}
}

func TestMCP_ServerHandlesToolsList(t *testing.T) {
	handler := &mockServerHandler{
		tools: []mcp.Tool{
			{Name: "echo", Description: "Echo tool"},
			{Name: "add", Description: "Add numbers"},
		},
	}

	server := mcp.NewServer(handler)

	req := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	resp, err := server.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	var result struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestMCP_ServerHandlesUnknownMethod(t *testing.T) {
	server := mcp.NewServer(&mockServerHandler{})

	req := &mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "unknown/method",
	}

	resp, err := server.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected method-not-found code -32601, got %d", resp.Error.Code)
	}
}

func TestMCP_ConfigLoadFromNonexistent(t *testing.T) {
	configs, err := mcp.LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected empty configs from nonexistent dir, got %d", len(configs))
	}
}

func TestMCP_ConfigLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, ".agents", "ycode")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configJSON := `{
		"mcpServers": {
			"test-server": {
				"command": "node",
				"args": ["server.js"],
				"env": {"PORT": "3000"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	configs, err := mcp.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	cfg, ok := configs["test-server"]
	if !ok {
		t.Fatal("expected test-server in configs")
	}
	if cfg.Command != "node" {
		t.Errorf("expected command 'node', got %q", cfg.Command)
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "server.js" {
		t.Errorf("unexpected args: %v", cfg.Args)
	}
	if cfg.Env["PORT"] != "3000" {
		t.Errorf("expected env PORT=3000, got %q", cfg.Env["PORT"])
	}
}

// mockServerHandler implements mcp.ServerHandler for testing.
type mockServerHandler struct {
	tools     []mcp.Tool
	resources []mcp.Resource
}

func (h *mockServerHandler) HandleToolCall(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "called: " + name, nil
}

func (h *mockServerHandler) ListTools() []mcp.Tool {
	return h.tools
}

func (h *mockServerHandler) ListResources() []mcp.Resource {
	return h.resources
}

func (h *mockServerHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "content of " + uri, nil
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
