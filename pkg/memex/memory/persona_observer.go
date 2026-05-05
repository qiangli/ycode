package memory

import (
	"strings"
	"time"
)

// ToolOutcome records what happened with a tool call in a turn.
type ToolOutcome struct {
	ToolName  string
	Approved  bool // user approved the tool call
	Corrected bool // user corrected the output afterward
}

// ObserveTurn extracts behavioral signals from a single conversation turn.
// This is a pure function with no side effects — it only analyzes input.
func ObserveTurn(userMessage string, toolOutcomes []ToolOutcome, turnNumber int) SessionSignal {
	now := time.Now()
	words := strings.Fields(strings.ToLower(userMessage))

	sig := SessionSignal{
		TurnNumber:       turnNumber,
		MessageLength:    len(words),
		QuestionCount:    countQuestions(userMessage, words),
		TechnicalDensity: technicalDensity(words),
		Corrections:      countCorrections(words),
		DetectedIntent:   detectIntent(words),
		Timestamp:        now,
	}

	for _, to := range toolOutcomes {
		if to.Approved {
			sig.ToolApprovals++
		} else {
			sig.ToolDenials++
		}
		if to.Corrected {
			sig.Corrections++
		}
	}

	return sig
}

// countQuestions counts the number of questions in the message.
func countQuestions(raw string, words []string) int {
	count := strings.Count(raw, "?")

	// Also count question-word sentence starters.
	for _, w := range questionPrefixes {
		if len(words) > 0 && words[0] == w {
			count++
			break // only count the first word once
		}
	}
	return count
}

var questionPrefixes = []string{
	"how", "why", "what", "when", "where", "which",
	"can", "could", "would", "should", "does", "is",
}

// technicalDensity returns the ratio of technical terms to total words.
func technicalDensity(words []string) float64 {
	if len(words) == 0 {
		return 0
	}
	techCount := 0
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}") // strip punctuation
		if _, ok := technicalTerms[w]; ok {
			techCount++
		}
	}
	return float64(techCount) / float64(len(words))
}

// countCorrections detects correction patterns in the message.
func countCorrections(words []string) int {
	count := 0
	msg := strings.Join(words, " ")

	for _, pattern := range correctionPatterns {
		if strings.Contains(msg, pattern) {
			count++
		}
	}
	return count
}

var correctionPatterns = []string{
	"no, i meant",
	"not what i",
	"that's wrong",
	"thats wrong",
	"that is wrong",
	"undo that",
	"revert that",
	"go back",
	"try again",
	"not correct",
	"incorrect",
	"no that's not",
	"no thats not",
	"wrong approach",
	"don't do that",
	"dont do that",
	"stop doing",
}

// detectIntent infers the user's current intent from keyword clusters.
func detectIntent(words []string) string {
	msg := strings.Join(words, " ")

	type intentScore struct {
		intent string
		score  int
	}

	scores := []intentScore{
		{"debugging", countKeywords(msg, debugKeywords)},
		{"learning", countKeywords(msg, learnKeywords)},
		{"architecting", countKeywords(msg, architectKeywords)},
		{"reviewing", countKeywords(msg, reviewKeywords)},
	}

	best := intentScore{}
	for _, s := range scores {
		if s.score > best.score {
			best = s
		}
	}

	if best.score >= 2 {
		return best.intent
	}
	// Single keyword match only if there's no competition.
	if best.score == 1 {
		competing := 0
		for _, s := range scores {
			if s.score == 1 {
				competing++
			}
		}
		if competing == 1 {
			return best.intent
		}
	}
	return ""
}

func countKeywords(msg string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			count++
		}
	}
	return count
}

var debugKeywords = []string{
	"debug", "fix", "error", "bug", "crash", "panic",
	"stack trace", "stacktrace", "segfault", "nil pointer",
	"failed", "failing", "broken", "exception", "traceback",
}

var learnKeywords = []string{
	"explain", "teach", "how does", "how do", "what is",
	"what are", "understand", "learn", "tutorial", "example",
	"walk me through", "help me understand",
}

var architectKeywords = []string{
	"design", "architect", "should we", "structure",
	"pattern", "tradeoff", "trade-off", "approach",
	"scalab", "refactor", "reorganiz",
}

var reviewKeywords = []string{
	"review", "looks good", "lgtm", "approve",
	"comment on", "feedback on", "pr ", "pull request",
	"diff", "changes look",
}

// technicalTerms is a set of programming-related terms for density scoring.
// Built as a package-level variable for efficiency.
var technicalTerms = buildTechnicalTerms()

func buildTechnicalTerms() map[string]struct{} {
	terms := []string{
		// Languages and runtimes.
		"go", "golang", "python", "javascript", "typescript", "rust", "java",
		"ruby", "c++", "swift", "kotlin", "scala", "elixir", "haskell",
		"node", "deno", "bun",
		// Go-specific.
		"goroutine", "channel", "interface", "struct", "defer", "panic",
		"recover", "mutex", "waitgroup", "context", "errgroup",
		// General programming.
		"function", "method", "class", "module", "package", "import",
		"export", "variable", "constant", "type", "generic", "enum",
		"trait", "protocol", "abstract", "concrete",
		// Infrastructure.
		"api", "rest", "grpc", "graphql", "http", "https", "tcp", "udp",
		"websocket", "endpoint", "middleware", "handler", "router",
		"database", "sql", "nosql", "postgres", "mysql", "sqlite",
		"redis", "mongo", "kafka", "rabbitmq", "nats",
		"docker", "podman", "container", "kubernetes", "k8s", "helm",
		"terraform", "ansible", "ci", "cd", "pipeline",
		// Tools and practices.
		"git", "commit", "branch", "merge", "rebase", "pr",
		"test", "unittest", "benchmark", "lint", "fmt", "vet",
		"build", "compile", "deploy", "release",
		"debug", "breakpoint", "profil", "trace",
		// Architecture.
		"microservice", "monolith", "serverless", "lambda",
		"queue", "pubsub", "event", "stream", "batch",
		"cache", "index", "shard", "replica", "partition",
		// Data structures and algorithms.
		"array", "slice", "map", "hashmap", "tree", "graph",
		"stack", "queue", "heap", "trie", "btree",
		"algorithm", "recursive", "iteration", "concurrency", "parallel",
		"async", "await", "promise", "future", "callback",
	}

	m := make(map[string]struct{}, len(terms))
	for _, t := range terms {
		m[t] = struct{}{}
	}
	return m
}
