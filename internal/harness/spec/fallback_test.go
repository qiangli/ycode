package spec

import (
	"strings"
	"testing"
)

func TestFallbackRunRequiresExplicitBound(t *testing.T) {
	pipelines := map[string]Pipeline{
		"main": {Concurrency: 1, Nodes: []Stage{{ID: "route", Run: Run{Fallback: &FallbackRun{
			MaxAttempts: 2,
			Attempts:    []FallbackAttempt{{PipelineRef: "child", On: []string{"failed"}}},
			NoMatch:     "fail",
		}}}}},
		"child": {Concurrency: 1, Nodes: []Stage{{ID: "done", Run: Run{Stage: "lifecycle.transition"}}}},
	}
	err := validatePipelines(pipelines)
	if err == nil || !strings.Contains(err.Error(), "explicitly bounded") {
		t.Fatalf("error = %v", err)
	}
}

func TestFallbackRunRequiresOutcomeClasses(t *testing.T) {
	pipelines := map[string]Pipeline{
		"main": {Concurrency: 1, Nodes: []Stage{{ID: "route", Run: Run{Fallback: &FallbackRun{
			MaxAttempts: 1,
			Attempts:    []FallbackAttempt{{PipelineRef: "child"}},
			NoMatch:     "fail",
		}}}}},
		"child": {Concurrency: 1, Nodes: []Stage{{ID: "done", Run: Run{Stage: "lifecycle.transition"}}}},
	}
	if err := validatePipelines(pipelines); err != nil {
		t.Fatal(err)
	}
	err := validatePipelineContracts(pipelines)
	if err == nil || !strings.Contains(err.Error(), "has no outcome classes") {
		t.Fatalf("error = %v", err)
	}
}
