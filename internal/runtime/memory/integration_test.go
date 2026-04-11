package memory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// ---------------------------------------------------------------------------
// VCR / Recorded-Fixture Integration Tests
//
// These tests simulate the LLM → tool-dispatch → memory-persistence pipeline
// by replaying recorded API response fixtures. No live LLM calls are made.
//
// Fixture format (JSON):
//
//	{
//	  "description": "...",
//	  "response": {
//	    "content": [
//	      {"type": "text", "text": "..."},
//	      {"type": "tool_use", "id": "...", "name": "memory_save", "input": {...}}
//	    ]
//	  }
//	}
//
// The test loads the fixture, extracts tool_use blocks, dispatches them through
// a local tool handler backed by the real memory.Manager, and verifies that
// the expected side effects occurred (file written, index updated, etc.).
// ---------------------------------------------------------------------------

// fixtureResponse represents a recorded LLM response fixture.
type fixtureResponse struct {
	Description string         `json:"description"`
	Response    fixtureMessage `json:"response"`
}

type fixtureMessage struct {
	ID         string                `json:"id"`
	Content    []fixtureContentBlock `json:"content"`
	StopReason string                `json:"stop_reason"`
}

type fixtureContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// memoryToolHandler is a minimal tool dispatcher that handles memory_save,
// memory_recall, and memory_forget by delegating to a real memory.Manager.
type memoryToolHandler struct {
	mgr *memory.Manager
}

type saveInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Content     string `json:"content"`
}

type recallInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

type forgetInput struct {
	Name string `json:"name"`
}

func (h *memoryToolHandler) dispatch(_ context.Context, toolName string, input json.RawMessage) (string, error) {
	switch toolName {
	case "memory_save":
		var params saveInput
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse save input: %w", err)
		}
		mem := &memory.Memory{
			Name:        params.Name,
			Description: params.Description,
			Type:        memory.Type(params.Type),
			Content:     params.Content,
		}
		if err := h.mgr.Save(mem); err != nil {
			return "", err
		}
		return fmt.Sprintf("Saved memory %q", params.Name), nil

	case "memory_recall":
		var params recallInput
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse recall input: %w", err)
		}
		if params.MaxResults == 0 {
			params.MaxResults = 5
		}
		results, err := h.mgr.Recall(params.Query, params.MaxResults)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for _, r := range results {
			fmt.Fprintf(&sb, "- %s (score=%.2f): %s\n", r.Memory.Name, r.Score, r.Memory.Description)
		}
		if sb.Len() == 0 {
			return "No memories found.", nil
		}
		return sb.String(), nil

	case "memory_forget":
		var params forgetInput
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse forget input: %w", err)
		}
		if err := h.mgr.Forget(params.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("Forgot memory %q", params.Name), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

func loadFixture(t *testing.T, name string) *fixtureResponse {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	var fixture fixtureResponse
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return &fixture
}

func extractToolCalls(fixture *fixtureResponse) []fixtureContentBlock {
	var calls []fixtureContentBlock
	for _, block := range fixture.Response.Content {
		if block.Type == "tool_use" {
			calls = append(calls, block)
		}
	}
	return calls
}

// ---------------------------------------------------------------------------
// Test: Save user memory via fixture replay
// ---------------------------------------------------------------------------

func TestFixture_SaveUserMemory(t *testing.T) {
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler := &memoryToolHandler{mgr: mgr}

	fixture := loadFixture(t, "save_user_memory.json")
	calls := extractToolCalls(fixture)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}

	ctx := context.Background()
	result, err := handler.dispatch(ctx, calls[0].Name, calls[0].Input)
	if err != nil {
		t.Fatalf("dispatch save: %v", err)
	}
	if !strings.Contains(result, "user-coding-prefs") {
		t.Errorf("expected result to mention saved memory name, got: %s", result)
	}

	// Verify memory was actually persisted.
	all, err := mgr.All()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}
	if all[0].Name != "user-coding-prefs" {
		t.Errorf("expected name 'user-coding-prefs', got %q", all[0].Name)
	}
	if all[0].Type != memory.TypeUser {
		t.Errorf("expected type 'user', got %q", all[0].Type)
	}
	if !strings.Contains(all[0].Content, "tabs over spaces") {
		t.Errorf("expected content about tabs, got: %s", all[0].Content)
	}

	// Verify index was updated.
	idx, _ := mgr.ReadIndex()
	if !strings.Contains(idx, "user-coding-prefs") {
		t.Errorf("index should contain saved memory entry, got:\n%s", idx)
	}
}

// ---------------------------------------------------------------------------
// Test: Save feedback memory via fixture replay
// ---------------------------------------------------------------------------

func TestFixture_SaveFeedbackMemory(t *testing.T) {
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler := &memoryToolHandler{mgr: mgr}

	fixture := loadFixture(t, "save_feedback_memory.json")
	calls := extractToolCalls(fixture)

	ctx := context.Background()
	_, err = handler.dispatch(ctx, calls[0].Name, calls[0].Input)
	if err != nil {
		t.Fatalf("dispatch save feedback: %v", err)
	}

	all, _ := mgr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}
	if all[0].Type != memory.TypeFeedback {
		t.Errorf("expected type 'feedback', got %q", all[0].Type)
	}
	if !strings.Contains(all[0].Content, "real database") {
		t.Errorf("expected content about real database, got: %s", all[0].Content)
	}
}

// ---------------------------------------------------------------------------
// Test: Full save → recall → forget lifecycle via fixtures
// ---------------------------------------------------------------------------

func TestFixture_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler := &memoryToolHandler{mgr: mgr}
	ctx := context.Background()

	// Step 1: Save via fixture.
	saveFixture := loadFixture(t, "save_user_memory.json")
	saveCalls := extractToolCalls(saveFixture)
	_, err = handler.dispatch(ctx, saveCalls[0].Name, saveCalls[0].Input)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Step 2: Recall via fixture.
	recallFixture := loadFixture(t, "recall_memory.json")
	recallCalls := extractToolCalls(recallFixture)
	recallResult, err := handler.dispatch(ctx, recallCalls[0].Name, recallCalls[0].Input)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(recallResult, "user-coding-prefs") {
		t.Errorf("recall should find saved memory, got: %s", recallResult)
	}

	// Step 3: Forget via fixture.
	forgetFixture := loadFixture(t, "forget_memory.json")
	forgetCalls := extractToolCalls(forgetFixture)
	_, err = handler.dispatch(ctx, forgetCalls[0].Name, forgetCalls[0].Input)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}

	// Step 4: Verify gone.
	all, _ := mgr.All()
	if len(all) != 0 {
		t.Errorf("expected 0 memories after forget, got %d", len(all))
	}

	// Step 5: Recall again — should return nothing.
	recallResult, err = handler.dispatch(ctx, recallCalls[0].Name, recallCalls[0].Input)
	if err != nil {
		t.Fatalf("recall after forget: %v", err)
	}
	if recallResult != "No memories found." {
		t.Errorf("expected no memories after forget, got: %s", recallResult)
	}
}

// ---------------------------------------------------------------------------
// Test: Cross-session persistence (save in one "session", recall in another)
// ---------------------------------------------------------------------------

func TestFixture_CrossSessionPersistence(t *testing.T) {
	// Use a shared directory to simulate cross-session persistence.
	dir := t.TempDir()
	ctx := context.Background()

	// Session 1: Save a memory.
	{
		mgr, _ := memory.NewManager(dir)
		handler := &memoryToolHandler{mgr: mgr}
		fixture := loadFixture(t, "save_user_memory.json")
		calls := extractToolCalls(fixture)
		_, err := handler.dispatch(ctx, calls[0].Name, calls[0].Input)
		if err != nil {
			t.Fatalf("session 1 save: %v", err)
		}
	}

	// Session 2: New manager instance (simulates new session), recall the memory.
	{
		mgr, _ := memory.NewManager(dir)
		handler := &memoryToolHandler{mgr: mgr}
		fixture := loadFixture(t, "recall_memory.json")
		calls := extractToolCalls(fixture)
		result, err := handler.dispatch(ctx, calls[0].Name, calls[0].Input)
		if err != nil {
			t.Fatalf("session 2 recall: %v", err)
		}
		if !strings.Contains(result, "user-coding-prefs") {
			t.Errorf("session 2 should recall memory from session 1, got: %s", result)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple fixture replays — verify accumulation
// ---------------------------------------------------------------------------

func TestFixture_MultipleMemories(t *testing.T) {
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler := &memoryToolHandler{mgr: mgr}
	ctx := context.Background()

	// Save user memory.
	f1 := loadFixture(t, "save_user_memory.json")
	for _, call := range extractToolCalls(f1) {
		handler.dispatch(ctx, call.Name, call.Input)
	}

	// Save feedback memory.
	f2 := loadFixture(t, "save_feedback_memory.json")
	for _, call := range extractToolCalls(f2) {
		handler.dispatch(ctx, call.Name, call.Input)
	}

	// Should have 2 memories of different types.
	all, _ := mgr.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(all))
	}

	types := map[memory.Type]bool{}
	for _, m := range all {
		types[m.Type] = true
	}
	if !types[memory.TypeUser] {
		t.Error("expected a user type memory")
	}
	if !types[memory.TypeFeedback] {
		t.Error("expected a feedback type memory")
	}

	// Index should have both entries.
	idx, _ := mgr.ReadIndex()
	if !strings.Contains(idx, "user-coding-prefs") {
		t.Error("index missing user memory entry")
	}
	if !strings.Contains(idx, "feedback-no-db-mocks") {
		t.Error("index missing feedback memory entry")
	}
}

// ---------------------------------------------------------------------------
// Test: Replay with mock HTTP server (VCR-style SSE replay)
// ---------------------------------------------------------------------------

// TestVCR_MockServerReplay demonstrates replaying fixtures through a tool
// handler backed by the real memory.Manager. Fixtures are ordered so that
// saves run before recalls and forgets (simulating a real conversation).
func TestVCR_MockServerReplay(t *testing.T) {
	dir := t.TempDir()
	mgr, err := memory.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler := &memoryToolHandler{mgr: mgr}

	// Ordered fixture sequence: saves first, then recall, then forget.
	orderedFixtures := []string{
		"save_user_memory.json",
		"save_feedback_memory.json",
		"recall_memory.json",
		"forget_memory.json",
	}

	ctx := context.Background()
	for _, name := range orderedFixtures {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			var fixture fixtureResponse
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatalf("parse fixture: %v", err)
			}

			// Dispatch all tool_use blocks.
			for _, block := range fixture.Response.Content {
				if block.Type != "tool_use" {
					continue
				}

				result, err := handler.dispatch(ctx, block.Name, block.Input)
				if err != nil {
					t.Errorf("dispatch %s: %v", block.Name, err)
					continue
				}
				if result == "" {
					t.Errorf("dispatch %s returned empty result", block.Name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Fixture format validation
// ---------------------------------------------------------------------------

func TestFixtures_ValidFormat(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/fixtures/*.json")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	for _, f := range fixtures {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			var fixture fixtureResponse
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			if fixture.Description == "" {
				t.Error("fixture must have a description")
			}

			hasToolUse := false
			for _, block := range fixture.Response.Content {
				if block.Type == "tool_use" {
					hasToolUse = true
					if block.Name == "" {
						t.Error("tool_use block must have a name")
					}
					if block.ID == "" {
						t.Error("tool_use block must have an ID")
					}
					if block.Input == nil {
						t.Error("tool_use block must have input")
					}
				}
			}
			if !hasToolUse {
				t.Error("fixture must contain at least one tool_use block")
			}
		})
	}
}
