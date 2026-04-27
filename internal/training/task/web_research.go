package task

import (
	"fmt"
	"strings"
)

// WebResearchTask implements multi-step web research tasks.
type WebResearchTask struct {
	examples []Example
}

// NewWebResearchTask creates web research tasks.
func NewWebResearchTask() *WebResearchTask {
	return &WebResearchTask{
		examples: []Example{
			{
				ID:       "webres-001",
				Prompt:   "Research: What is the current population of Tokyo, Japan? Search the web for the most recent data and cite your source.",
				Expected: "13",
				Metadata: map[string]string{"requires_tools": "web_search,web_extract"},
			},
			{
				ID:       "webres-002",
				Prompt:   "Research: Who won the most recent Nobel Prize in Physics? Search the web and provide the name(s) and their contribution.",
				Expected: "Nobel",
				Metadata: map[string]string{"requires_tools": "web_search,web_extract"},
			},
			{
				ID:       "webres-003",
				Prompt:   "Research: What programming language is most popular according to the latest TIOBE index? Search for current data.",
				Expected: "Python",
				Metadata: map[string]string{"requires_tools": "web_search"},
			},
		},
	}
}

func (w *WebResearchTask) Name() string { return "web_research" }
func (w *WebResearchTask) Len() int     { return len(w.examples) }

func (w *WebResearchTask) GetExample(index int) (*Example, error) {
	if index < 0 || index >= len(w.examples) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(w.examples))
	}
	e := w.examples[index]
	return &e, nil
}

// Evaluate uses keyword matching as a heuristic.
// Full evaluation would use an LLM judge (reward/llm_judge.go).
func (w *WebResearchTask) Evaluate(example *Example, completion string) (float64, error) {
	if strings.Contains(completion, example.Expected) {
		return 1.0, nil
	}
	return 0.0, nil
}
