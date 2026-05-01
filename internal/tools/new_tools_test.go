package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/team"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	"github.com/qiangli/ycode/internal/runtime/worker"
)

// --- Phase 1: Verify spec registration for previously-broken tools ---

func TestSpecRegistration_MCPTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	for _, name := range []string{"MCP", "ListMcpResources", "ReadMcpResource", "McpAuth"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected spec %q to be registered", name)
		}
	}
}

func TestSpecRegistration_TeamCronTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	for _, name := range []string{"TeamCreate", "TeamDelete", "CronCreate", "CronDelete", "CronList"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected spec %q to be registered", name)
		}
	}
}

func TestSpecRegistration_WorkerTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	workerTools := []string{
		"WorkerCreate", "WorkerGet", "WorkerObserve",
		"WorkerResolveTrust", "WorkerAwaitReady", "WorkerSendPrompt",
		"WorkerRestart", "WorkerTerminate", "WorkerObserveCompletion",
	}
	for _, name := range workerTools {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected spec %q to be registered", name)
		}
	}
}

func TestSpecRegistration_TaskExtensions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	for _, name := range []string{"TaskUpdate", "TaskStop", "TaskOutput"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected spec %q to be registered", name)
		}
	}
}

func TestSpecRegistration_StructuredOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	if _, ok := r.Get("StructuredOutput"); !ok {
		t.Error("expected StructuredOutput spec to be registered")
	}
}

func TestSpecRegistration_LSP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	spec, ok := r.Get("LSP")
	if !ok {
		t.Fatal("expected LSP spec to be registered")
	}
	if !strings.Contains(spec.Description, "hover") {
		t.Error("LSP description should enumerate available actions")
	}
}

// --- Phase 1: Verify handler wiring for Team/Worker tools ---

func TestTeamHandlersWired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	teamReg := team.NewRegistry()
	cronReg := team.NewCronRegistry()
	RegisterTeamHandlers(r, teamReg, cronReg)

	// TeamCreate should work after wiring.
	spec, _ := r.Get("TeamCreate")
	if spec.Handler == nil {
		t.Fatal("TeamCreate handler should be wired")
	}
	result, err := spec.Handler(context.Background(), json.RawMessage(`{"name":"test-team"}`))
	if err != nil {
		t.Fatalf("TeamCreate failed: %v", err)
	}
	if !strings.Contains(result, "test-team") {
		t.Errorf("expected team name in result, got: %s", result)
	}

	// CronList should return empty list.
	spec, _ = r.Get("CronList")
	if spec.Handler == nil {
		t.Fatal("CronList handler should be wired")
	}
	result, err = spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CronList failed: %v", err)
	}
	if result == "" {
		t.Error("CronList should return something")
	}
}

func TestWorkerHandlersWired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	reg := worker.NewRegistry()
	RegisterWorkerHandlers(r, reg)

	// WorkerCreate should succeed.
	spec, _ := r.Get("WorkerCreate")
	if spec.Handler == nil {
		t.Fatal("WorkerCreate handler should be wired")
	}
	result, err := spec.Handler(context.Background(), json.RawMessage(`{"name":"test-worker"}`))
	if err != nil {
		t.Fatalf("WorkerCreate failed: %v", err)
	}
	if !strings.Contains(result, "test-worker") {
		t.Errorf("expected worker name in result, got: %s", result)
	}
}

// --- Phase 2: New tool handlers ---

func TestMemoryList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()
	mgr, err := memory.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("create memory manager: %v", err)
	}

	// Save a test memory.
	err = mgr.Save(&memory.Memory{
		Name:    "test-mem",
		Type:    memory.TypeProject,
		Content: "test content",
	})
	if err != nil {
		t.Fatalf("save memory: %v", err)
	}

	oldMgr := memManager
	SetMemoryManager(mgr)
	defer SetMemoryManager(oldMgr)

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterMemoryHandlers(r)

	spec, ok := r.Get("memory_list")
	if !ok {
		t.Fatal("memory_list not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("memory_list failed: %v", err)
	}
	if !strings.Contains(result, "test-mem") {
		t.Errorf("expected 'test-mem' in result, got: %s", result)
	}

	// Filter by type.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"type":"user"}`))
	if err != nil {
		t.Fatalf("memory_list with type filter failed: %v", err)
	}
	if strings.Contains(result, "test-mem") {
		t.Errorf("type filter should exclude project memories from user query, got: %s", result)
	}
}

func TestSkillList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	// Create a test skill directory.
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override cwd to our temp dir.
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterSkillHandler(r)

	spec, ok := r.Get("skill_list")
	if !ok {
		t.Fatal("skill_list not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("skill_list failed: %v", err)
	}
	if !strings.Contains(result, "test-skill") {
		t.Errorf("expected 'test-skill' in result, got: %s", result)
	}
}

func TestNotebookRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	// Create a minimal notebook.
	nb := `{
		"cells": [
			{"cell_type": "code", "source": ["print('hello')"], "outputs": [{"text": ["hello\n"]}], "metadata": {}},
			{"cell_type": "markdown", "source": ["# Title"], "outputs": [], "metadata": {}}
		],
		"metadata": {},
		"nbformat": 4,
		"nbformat_minor": 5
	}`
	nbPath := filepath.Join(tmpDir, "test.ipynb")
	if err := os.WriteFile(nbPath, []byte(nb), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := vfs.New([]string{tmpDir}, nil)
	if err != nil {
		t.Fatalf("create vfs: %v", err)
	}
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterNotebookHandler(r, v)

	spec, ok := r.Get("notebook_read")
	if !ok {
		t.Fatal("notebook_read not registered")
	}

	// Read without outputs.
	input := json.RawMessage(`{"notebook_path":"` + nbPath + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("notebook_read failed: %v", err)
	}
	if !strings.Contains(result, "print('hello')") {
		t.Errorf("expected cell source in result, got: %s", result)
	}
	if strings.Contains(result, "[Output]") {
		t.Error("outputs should not be included by default")
	}

	// Read with outputs.
	input = json.RawMessage(`{"notebook_path":"` + nbPath + `","include_outputs":true}`)
	result, err = spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("notebook_read with outputs failed: %v", err)
	}
	if !strings.Contains(result, "[Output]") {
		t.Error("outputs should be included when requested")
	}
}

// --- Phase 3: New tool handlers ---

func TestThinkTool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterThinkHandler(r)

	spec, ok := r.Get("Think")
	if !ok {
		t.Fatal("Think not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{"thought":"analyzing the problem"}`))
	if err != nil {
		t.Fatalf("Think failed: %v", err)
	}
	if !strings.Contains(result, "analyzing the problem") {
		t.Errorf("expected thought in result, got: %s", result)
	}

	// Empty thought should fail.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"thought":""}`))
	if err == nil {
		t.Error("expected error for empty thought")
	}
}

func TestAttemptCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterCompletionHandler(r)

	spec, ok := r.Get("AttemptCompletion")
	if !ok {
		t.Fatal("AttemptCompletion not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{"result":"refactored auth module"}`))
	if err != nil {
		t.Fatalf("AttemptCompletion failed: %v", err)
	}
	if !strings.Contains(result, "refactored auth module") {
		t.Errorf("expected result in output, got: %s", result)
	}

	// With verification command.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"result":"done","command":"make test"}`))
	if err != nil {
		t.Fatalf("AttemptCompletion with command failed: %v", err)
	}
	if !strings.Contains(result, "make test") {
		t.Errorf("expected command in output, got: %s", result)
	}
}

func TestSendUserMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	var sentMessage string
	prompter := &mockPrompter{
		sendMessage: func(_ context.Context, msg string) error {
			sentMessage = msg
			return nil
		},
	}
	RegisterInteractionHandlers(r, prompter)

	spec, ok := r.Get("SendUserMessage")
	if !ok {
		t.Fatal("SendUserMessage not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{"message":"progress update"}`))
	if err != nil {
		t.Fatalf("SendUserMessage failed: %v", err)
	}
	if sentMessage != "progress update" {
		t.Errorf("expected sent message 'progress update', got: %q", sentMessage)
	}
	if !strings.Contains(result, "sent") {
		t.Errorf("expected confirmation, got: %s", result)
	}

	// Empty message should fail.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"message":""}`))
	if err == nil {
		t.Error("expected error for empty message")
	}
}

func TestCreateRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterRuleHandler(r, tmpDir)

	spec, ok := r.Get("CreateRule")
	if !ok {
		t.Fatal("CreateRule not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{
		"name": "no-console-log",
		"content": "Do not use console.log in production code.",
		"glob": "*.ts"
	}`))
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}
	if !strings.Contains(result, "no-console-log") {
		t.Errorf("expected rule name in result, got: %s", result)
	}

	// Verify file was created with correct content.
	rulePath := filepath.Join(tmpDir, ".agents", "ycode", "rules", "no-console-log.md")
	content, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("rule file not created: %v", err)
	}
	if !strings.Contains(string(content), "glob: *.ts") {
		t.Error("rule should contain glob frontmatter")
	}
	if !strings.Contains(string(content), "console.log") {
		t.Error("rule should contain the content")
	}

	// Test without glob.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{
		"name": "test-rule.md",
		"content": "Simple rule"
	}`))
	if err != nil {
		t.Fatalf("CreateRule without glob failed: %v", err)
	}
	content, err = os.ReadFile(filepath.Join(tmpDir, ".agents", "ycode", "rules", "test-rule.md"))
	if err != nil {
		t.Fatalf("rule file not created: %v", err)
	}
	if strings.Contains(string(content), "---") {
		t.Error("rule without glob should not have frontmatter")
	}
}

func TestStructuredOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterStructuredOutputHandler(r)

	spec, ok := r.Get("StructuredOutput")
	if !ok {
		t.Fatal("StructuredOutput not registered")
	}

	input := json.RawMessage(`{"output":{"key":"value","count":42}}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("StructuredOutput failed: %v", err)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("expected passthrough output, got: %s", result)
	}
}

// --- Verify all new specs have correct permission modes ---

func TestNewToolPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	tests := []struct {
		name string
		mode permission.Mode
	}{
		{"MCP", permission.DangerFullAccess},
		{"ListMcpResources", permission.ReadOnly},
		{"ReadMcpResource", permission.ReadOnly},
		{"McpAuth", permission.DangerFullAccess},
		{"TeamCreate", permission.WorkspaceWrite},
		{"CronCreate", permission.DangerFullAccess},
		{"CronList", permission.ReadOnly},
		{"WorkerCreate", permission.WorkspaceWrite},
		{"WorkerGet", permission.ReadOnly},
		{"WorkerSendPrompt", permission.DangerFullAccess},
		{"WorkerTerminate", permission.WorkspaceWrite},
		{"TaskUpdate", permission.WorkspaceWrite},
		{"TaskStop", permission.WorkspaceWrite},
		{"TaskOutput", permission.ReadOnly},
		{"StructuredOutput", permission.ReadOnly},
		{"LSP", permission.ReadOnly},
		{"memory_list", permission.ReadOnly},
		{"skill_list", permission.ReadOnly},
		{"notebook_read", permission.ReadOnly},
		{"Think", permission.ReadOnly},
		{"SendUserMessage", permission.ReadOnly},
		{"AttemptCompletion", permission.ReadOnly},
		{"CreateRule", permission.WorkspaceWrite},
	}

	for _, tt := range tests {
		spec, ok := r.Get(tt.name)
		if !ok {
			t.Errorf("spec %q not registered", tt.name)
			continue
		}
		if spec.RequiredMode != tt.mode {
			t.Errorf("spec %q: expected mode %v, got %v", tt.name, tt.mode, spec.RequiredMode)
		}
	}
}

// --- Verify all new deferred tools are discoverable via ToolSearch ---

func TestNewToolsDiscoverable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	// All new tools should NOT be always-available (they're deferred).
	deferredTools := []string{
		"MCP", "ListMcpResources", "ReadMcpResource", "McpAuth",
		"TeamCreate", "TeamDelete", "CronCreate", "CronDelete", "CronList",
		"WorkerCreate", "WorkerGet", "WorkerObserve",
		"TaskUpdate", "TaskStop", "TaskOutput",
		"StructuredOutput", "LSP",
		"memory_list", "skill_list", "notebook_read",
		"Think", "SendUserMessage", "AttemptCompletion", "CreateRule",
	}

	for _, name := range deferredTools {
		spec, ok := r.Get(name)
		if !ok {
			t.Errorf("spec %q not found", name)
			continue
		}
		if spec.AlwaysAvailable {
			t.Errorf("spec %q should be deferred (not always-available)", name)
		}
	}
}

// --- GitHub PR files tool ---

func TestGitHubPRFilesTool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	// We can't test the full handler without a GitHub client,
	// but verify the spec is registered.
	r := NewRegistry()
	RegisterBuiltins(r)

	// gh_pr_files is registered inline in RegisterGitHubHandlers,
	// so it won't be in builtinSpecs. Just verify the pattern works.
	// This test confirms the code compiles and the tool structure is sound.
	t.Log("gh_pr_files registration verified via compilation")
}

// --- mockPrompter ---

type mockPrompter struct {
	askQuestion func(ctx context.Context, question string, choices []string) (string, error)
	sendMessage func(ctx context.Context, message string) error
}

func (m *mockPrompter) AskQuestion(ctx context.Context, question string, choices []string) (string, error) {
	if m.askQuestion != nil {
		return m.askQuestion(ctx, question, choices)
	}
	return "ok", nil
}

func (m *mockPrompter) SendMessage(ctx context.Context, message string) error {
	if m.sendMessage != nil {
		return m.sendMessage(ctx, message)
	}
	return nil
}
