package conversation

import (
	"testing"
)

func TestNewResearchPlanV2(t *testing.T) {
	plan := NewResearchPlanV2("test query")
	if plan.OriginalQuery != "test query" {
		t.Fatalf("query = %q, want %q", plan.OriginalQuery, "test query")
	}
	if plan.Dependencies == nil {
		t.Fatal("Dependencies map should be initialized")
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("tasks = %d, want 0", len(plan.Tasks))
	}
}

func TestAddTask(t *testing.T) {
	plan := NewResearchPlanV2("q")

	plan.AddTask("t1", "query1", "Explore", nil)
	plan.AddTask("t2", "query2", "Plan", []string{"t1"})

	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "t1" || plan.Tasks[0].AgentType != "Explore" {
		t.Fatalf("task 0 mismatch: %+v", plan.Tasks[0])
	}
	if plan.Tasks[1].Status != "pending" {
		t.Fatalf("task status = %q, want pending", plan.Tasks[1].Status)
	}

	deps, ok := plan.Dependencies["t2"]
	if !ok || len(deps) != 1 || deps[0] != "t1" {
		t.Fatalf("t2 deps = %v, want [t1]", deps)
	}
	if _, ok := plan.Dependencies["t1"]; ok {
		t.Fatal("t1 should have no dependencies entry")
	}
}

func TestReady(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.AddTask("t2", "q2", "Plan", []string{"t1"})
	plan.AddTask("t3", "q3", "Explore", nil)

	// Initially t1 and t3 are ready (no deps).
	ready := plan.Ready()
	if len(ready) != 2 {
		t.Fatalf("ready = %d, want 2", len(ready))
	}
	ids := map[string]bool{}
	for _, r := range ready {
		ids[r.ID] = true
	}
	if !ids["t1"] || !ids["t3"] {
		t.Fatal("t1 and t3 should be ready")
	}

	// Complete t1 -> t2 becomes ready.
	plan.Tasks[0].Status = "completed"
	ready = plan.Ready()
	if len(ready) != 2 {
		t.Fatalf("ready after t1 done = %d, want 2", len(ready))
	}
	ids = map[string]bool{}
	for _, r := range ready {
		ids[r.ID] = true
	}
	if !ids["t2"] || !ids["t3"] {
		t.Fatal("t2 and t3 should be ready")
	}
}

func TestReadySkipsNonPending(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.Tasks[0].Status = "in_progress"

	ready := plan.Ready()
	if len(ready) != 0 {
		t.Fatalf("in_progress task should not be ready, got %d", len(ready))
	}
}

func TestIsComplete(t *testing.T) {
	plan := NewResearchPlanV2("q")
	// Empty plan is complete.
	if !plan.IsComplete() {
		t.Fatal("empty plan should be complete")
	}

	plan.AddTask("t1", "q", "Explore", nil)
	if plan.IsComplete() {
		t.Fatal("pending plan should not be complete")
	}

	plan.Tasks[0].Status = "completed"
	if !plan.IsComplete() {
		t.Fatal("all completed should be complete")
	}

	plan.AddTask("t2", "q2", "Plan", nil)
	plan.Tasks[1].Status = "failed"
	if !plan.IsComplete() {
		t.Fatal("failed tasks count as complete")
	}
}

func TestCompletedResults(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.AddTask("t2", "q2", "Plan", nil)
	plan.AddTask("t3", "q3", "Explore", nil)

	plan.Tasks[0].Status = "completed"
	plan.Tasks[0].Result = "result1"
	plan.Tasks[1].Status = "failed"
	plan.Tasks[2].Status = "completed"
	plan.Tasks[2].Result = "result3"

	results := plan.CompletedResults()
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0] != "result1" || results[1] != "result3" {
		t.Fatalf("results = %v", results)
	}
}

func TestCompletedResultsSkipsEmpty(t *testing.T) {
	plan := NewResearchPlanV2("q")
	plan.AddTask("t1", "q1", "Explore", nil)
	plan.Tasks[0].Status = "completed"
	plan.Tasks[0].Result = ""

	results := plan.CompletedResults()
	if len(results) != 0 {
		t.Fatalf("empty result should be skipped, got %d", len(results))
	}
}

func TestParseDecompositionValidJSON(t *testing.T) {
	response := `{
		"sub_queries": [
			{"id": "q1", "query": "What is Go?", "agent_type": "Explore"},
			{"id": "q2", "query": "Go concurrency", "agent_type": "Plan", "depends_on": ["q1"]}
		],
		"synthesis_prompt": "Combine findings about Go"
	}`

	plan, err := ParseDecomposition("original", response)
	if err != nil {
		t.Fatalf("ParseDecomposition: %v", err)
	}
	if plan.OriginalQuery != "original" {
		t.Fatalf("query = %q", plan.OriginalQuery)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Synthesizer != "Combine findings about Go" {
		t.Fatalf("synthesizer = %q", plan.Synthesizer)
	}
	deps := plan.Dependencies["q2"]
	if len(deps) != 1 || deps[0] != "q1" {
		t.Fatalf("q2 deps = %v", deps)
	}
}

func TestParseDecompositionMarkdownWrapped(t *testing.T) {
	response := "Here is the decomposition:\n```json\n" +
		`{"sub_queries": [{"id": "q1", "query": "test", "agent_type": "Explore"}], "synthesis_prompt": "synth"}` +
		"\n```\nDone."

	plan, err := ParseDecomposition("q", response)
	if err != nil {
		t.Fatalf("ParseDecomposition: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(plan.Tasks))
	}
}

func TestParseDecompositionNoJSON(t *testing.T) {
	_, err := ParseDecomposition("q", "no json here at all")
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
}

func TestParseDecompositionEmptySubQueries(t *testing.T) {
	response := `{"sub_queries": [], "synthesis_prompt": "none"}`
	_, err := ParseDecomposition("q", response)
	if err == nil {
		t.Fatal("expected error for empty sub_queries")
	}
}

func TestExtractJSONRaw(t *testing.T) {
	input := `some text {"key": "value"} more text`
	got := extractJSON(input)
	if got != `{"key": "value"}` {
		t.Fatalf("extractJSON = %q", got)
	}
}

func TestExtractJSONMarkdownCodeBlock(t *testing.T) {
	input := "```json\n{\"a\": 1}\n```"
	got := extractJSON(input)
	if got != `{"a": 1}` {
		t.Fatalf("extractJSON = %q", got)
	}
}

func TestExtractJSONNestedBraces(t *testing.T) {
	input := `{"outer": {"inner": "value"}}`
	got := extractJSON(input)
	if got != input {
		t.Fatalf("extractJSON = %q, want %q", got, input)
	}
}

func TestExtractJSONEmpty(t *testing.T) {
	got := extractJSON("no braces here")
	if got != "" {
		t.Fatalf("extractJSON = %q, want empty", got)
	}
}
