package conversation

import (
	"context"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/tools"
)

// Pre-activation constants.
const (
	// scoreThreshold is the minimum SearchTools score for auto-activation.
	// 12 = one exact name match, or name-contains(8) + description-contains(4).
	scoreThreshold = 12

	// maxScoredActivations caps tools activated via scoring to prevent schema bloat.
	maxScoredActivations = 5
)

// intentToolBundles maps high-precision keyword signals to tool bundles.
// Only keywords that are unambiguously domain-specific are included — words
// that almost never appear in casual speech without meaning the tool operation.
//
// Noisy keywords (error, delete, copy, task, note, slow, debug, etc.) are
// intentionally excluded. They are handled by Tier 1b (SearchTools scoring)
// or Tier 2 (LLM classification via InferenceRouter).
var intentToolBundles = map[string][]string{
	// Git operations — these keywords are domain-specific.
	"commit":   {"git_status", "git_log", "git_commit"},
	"branch":   {"git_status", "git_log", "git_branch"},
	"stash":    {"git_stash", "git_status"},
	"rebase":   {"git_status", "git_log", "git_branch"},
	"merge":    {"git_status", "git_log", "git_branch"},
	"cherry":   {"git_status", "git_log"},
	"git log":  {"git_status", "git_log"},
	"git diff": {"git_status", "view_diff"},

	// Deployment.
	"deploy": {"WebFetch", "RemoteTrigger", "query_metrics"},

	// Observability — these are tool-name-like terms.
	"metrics": {"query_metrics"},
	"traces":  {"query_traces"},

	// Memory and memos.
	"memo":  {"MemosStore", "MemosSearch", "MemosList"},
	"memos": {"MemosStore", "MemosSearch", "MemosList"},

	// Testing.
	"test":  {"run_tests"},
	"tests": {"run_tests"},

	// Scheduling.
	"cron": {"CronCreate", "CronList", "CronDelete"},

	// Planning and orchestration — activate when users describe complex tasks.
	"plan":        {"UpdatePlan", "ListPlan", "SetGoal", "EnterPlanMode"},
	"decompose":   {"UpdatePlan", "ParallelAgents", "SetGoal"},
	"break down":  {"UpdatePlan", "ParallelAgents", "SetGoal"},
	"parallel":    {"ParallelAgents", "UpdatePlan"},
	"concurrent":  {"ParallelAgents", "UpdatePlan"},
	"in parallel": {"ParallelAgents", "UpdatePlan"},
	"subtask":     {"UpdatePlan", "ParallelAgents"},
	"milestone":   {"UpdatePlan", "SetGoal", "ListPlan"},
	"roadmap":     {"UpdatePlan", "SetGoal", "ListPlan"},
	"goal":        {"SetGoal", "GetGoal", "UpdatePlan"},
	"implement":   {"UpdatePlan", "SetTaskStatus"},
	"refactor":    {"UpdatePlan", "SetTaskStatus"},
	"architect":   {"UpdatePlan", "SetGoal", "EnterPlanMode"},

	// Document reading.
	"pdf":         {"read_document"},
	"docx":        {"read_document"},
	"xlsx":        {"read_document"},
	"excel":       {"read_document"},
	"word":        {"read_document"},
	"powerpoint":  {"read_document"},
	"pptx":        {"read_document"},
	"spreadsheet": {"read_document"},

	// Agent orchestration.
	"subagent": {"ParallelAgents", "AgentList", "AgentWait"},
	"agent":    {"AgentList", "AgentWait", "AgentClose"},
	"worker":   {"WorkerCreate", "WorkerGet", "WorkerTerminate"},
}

// stopWords are common English words filtered from user messages before
// SearchTools scoring to reduce noise. These words appear frequently in
// tool descriptions and user messages but carry no intent signal.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"can": true, "may": true, "might": true, "shall": true,
	"i": true, "me": true, "my": true, "you": true, "your": true,
	"we": true, "our": true, "they": true, "them": true, "their": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "in": true, "on": true, "at": true, "to": true,
	"for": true, "of": true, "with": true, "from": true, "by": true,
	"and": true, "or": true, "not": true, "no": true, "but": true,
	"if": true, "so": true, "as": true, "up": true, "out": true,
	"about": true, "how": true, "what": true, "when": true, "where": true,
	"which": true, "who": true, "why": true, "please": true, "help": true,
	"just": true, "also": true, "very": true, "really": true, "actually": true,
	"some": true, "all": true, "any": true, "each": true, "every": true,
	"into": true, "over": true, "after": true, "before": true, "between": true,
	"there": true, "here": true, "then": true, "than": true, "more": true,
	"most": true, "other": true, "only": true, "own": true, "same": true,
	"need": true, "want": true, "like": true, "make": true, "get": true,
}

// preActivateTools runs the four-tier pre-activation pipeline:
//
//	Tier 1a: High-precision keyword matching (< 0.1ms)
//	Tier 1b: SearchTools() scoring with stop-word filter (< 1ms)
//	Tier 1c: Semantic vector similarity via SmartRouter (< 50ms if indexed)
//	Tier 2:  LLM classification via InferenceRouter (200-500ms, $0 if local)
//
// Tier 2 only runs if all earlier tiers activated zero tools AND an InferenceRouter
// is available. Returns the total number of tools activated across all tiers.
func (r *Runtime) preActivateTools(userMessage string) int {
	if userMessage == "" {
		return 0
	}

	// Tier 1a: keyword matching.
	total := r.preActivateByKeyword(userMessage)

	// Tier 1b: SearchTools scoring against tool names and descriptions.
	total += r.preActivateByScoring(userMessage)

	// Tier 1c: Semantic vector similarity via SmartRouter.
	total += r.preActivateBySemantic(userMessage)

	// Tier 2: LLM classification — only if all earlier tiers found nothing.
	if total == 0 {
		total += r.preActivateByClassification(userMessage)
	}

	// Post-step: Co-occurrence expansion.
	// After all tiers activate tools, expand using learned co-occurrence clusters.
	// If tool A was activated and historically A→B,C, also activate B and C.
	if total > 0 {
		total += r.expandByCoOccurrence()
	}

	if total > 0 {
		r.logger.Info("tool pre-activation", "total", total)
	}
	return total
}

// expandByCoOccurrence expands the set of activated tools using learned
// co-occurrence clusters. For each already-activated tool, adds tools that
// frequently follow it in historical sessions. Terminal tools (tools that
// are typically the last tool used) do not trigger expansion.
func (r *Runtime) expandByCoOccurrence() int {
	if r.coOccurrence == nil {
		return 0
	}

	// Snapshot the currently activated tools to avoid modifying during iteration.
	var triggers []string
	for name := range r.activatedTools {
		triggers = append(triggers, name)
	}

	activated := 0
	for _, trigger := range triggers {
		// Skip terminal tools — they don't lead to other tools.
		if r.coOccurrence.IsTerminal(trigger) {
			continue
		}

		followers := r.coOccurrence.CoActivate(trigger, 3)
		for _, follower := range followers {
			if _, exists := r.activatedTools[follower]; exists {
				continue
			}
			if _, ok := r.registry.Get(follower); !ok {
				continue
			}
			r.activatedTools[follower] = r.turnCount
			activated++
			r.logger.Debug("co-occurrence expansion", "trigger", trigger, "activated", follower)
		}
	}
	return activated
}

// preActivateBySemantic is Tier 1c: vector-based semantic tool matching.
// Uses the SmartRouter to find tools whose descriptions are semantically
// similar to the user message, boosted by user preference signals.
func (r *Runtime) preActivateBySemantic(userMessage string) int {
	if r.smartRouter == nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	toolNames := r.smartRouter.SelectTools(ctx, r.registry, userMessage, 5)
	if len(toolNames) == 0 {
		return 0
	}

	activated := 0
	for _, name := range toolNames {
		if _, exists := r.activatedTools[name]; exists {
			continue
		}
		if _, ok := r.registry.Get(name); !ok {
			continue
		}
		r.activatedTools[name] = r.turnCount
		activated++
		r.logger.Debug("pre-activated tool by semantic match", "tool", name)
	}
	return activated
}

// preActivateByKeyword is Tier 1a: high-precision keyword matching.
func (r *Runtime) preActivateByKeyword(userMessage string) int {
	lower := strings.ToLower(userMessage)
	activated := 0

	for keyword, toolNames := range intentToolBundles {
		if !strings.Contains(lower, keyword) {
			continue
		}
		for _, name := range toolNames {
			if _, exists := r.activatedTools[name]; exists {
				continue
			}
			if _, ok := r.registry.Get(name); !ok {
				continue
			}
			r.activatedTools[name] = r.turnCount
			activated++
			r.logger.Debug("pre-activated tool by keyword", "tool", name, "keyword", keyword)
		}
	}
	return activated
}

// preActivateByScoring is Tier 1b: SearchTools() scoring with stop-word filter.
// It calls the same scoring function that ToolSearch uses, but directly against
// the user message — zero LLM cost, < 1ms.
func (r *Runtime) preActivateByScoring(userMessage string) int {
	// Filter stop words from the user message to reduce scoring noise.
	filtered := filterStopWords(userMessage)
	if filtered == "" {
		return 0
	}

	scores := tools.SearchTools(r.registry, filtered, maxScoredActivations+5) // fetch extras to filter
	activated := 0

	for _, score := range scores {
		if score.Score < scoreThreshold {
			break // sorted descending, rest will be lower
		}
		if activated >= maxScoredActivations {
			break
		}
		name := score.Spec.Name
		if _, exists := r.activatedTools[name]; exists {
			continue // already activated by keyword tier
		}
		// Skip always-available tools — they don't need pre-activation.
		if score.Spec.AlwaysAvailable {
			continue
		}
		r.activatedTools[name] = r.turnCount
		activated++
		r.logger.Debug("pre-activated tool by scoring", "tool", name, "score", score.Score)
	}
	return activated
}

// preActivateByClassification is Tier 2: LLM-based classification via the
// InferenceRouter. Uses a lightweight model (local Ollama or cheap remote)
// to classify the user message into tool categories.
func (r *Runtime) preActivateByClassification(userMessage string) int {
	if r.inferenceRouter == nil {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	toolNames := r.inferenceRouter.ClassifyTools(ctx, userMessage)
	if len(toolNames) == 0 {
		return 0
	}

	activated := 0
	for _, name := range toolNames {
		if _, exists := r.activatedTools[name]; exists {
			continue
		}
		if _, ok := r.registry.Get(name); !ok {
			continue
		}
		r.activatedTools[name] = r.turnCount
		activated++
		r.logger.Debug("pre-activated tool by classification", "tool", name)
	}
	return activated
}

// filterStopWords removes common English words from a message,
// returning the remaining words as a space-separated string.
func filterStopWords(msg string) string {
	words := strings.Fields(strings.ToLower(msg))
	var kept []string
	for _, w := range words {
		// Strip punctuation from edges.
		w = strings.Trim(w, ".,;:!?\"'()[]{}/-")
		if w == "" || len(w) < 2 {
			continue
		}
		if stopWords[w] {
			continue
		}
		kept = append(kept, w)
	}
	return strings.Join(kept, " ")
}
