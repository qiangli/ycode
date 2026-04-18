package builtin

import (
	"regexp"
	"strings"
)

// IntentMatch represents a detected builtin intent from user input.
type IntentMatch struct {
	Operation string // builtin operation name: "commit", "pr", etc.
	Args      string // extracted hint/context for the operation
}

// DetectIntent examines user input for high-confidence builtin operation
// matches. Returns nil if no confident match is found — the caller should
// proceed with the normal LLM path.
//
// This is intentionally conservative: only unambiguous imperative requests
// match. Anything that looks like a question, explanation request, or
// references commit history/metadata is rejected.
func DetectIntent(input string) *IntentMatch {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	lower := strings.ToLower(input)

	// Reject questions and explanation requests early.
	if isQuestion(lower) {
		return nil
	}

	if m := matchCommitIntent(lower); m != nil {
		return m
	}

	return nil
}

// Commit intent patterns — only unambiguous imperative forms.
var commitPatterns = []*regexp.Regexp{
	// "commit", "commit changes", "commit my changes", "commit the changes"
	regexp.MustCompile(`^(?:please\s+)?commit(?:\s+(?:my|the|these|all|current|those))?\s*(?:changes?)?\s*$`),
	// "commit <hint>" — short imperative with context
	regexp.MustCompile(`^(?:please\s+)?commit\s+(?:my|the|these|all|current|those)\s+(?:changes?\s+)?(.+)$`),
	// "make a commit", "create a commit"
	regexp.MustCompile(`^(?:please\s+)?(?:make|create)\s+a?\s*commit\s*$`),
	// "stage and commit", "add and commit"
	regexp.MustCompile(`^(?:please\s+)?(?:stage|add)\s+and\s+commit(?:\s+(.*))?$`),
	// "commit this", "commit that", "commit it"
	regexp.MustCompile(`^(?:please\s+)?commit\s+(?:this|that|it|everything)\s*$`),
	// "go ahead and commit", "ok commit"
	regexp.MustCompile(`^(?:ok|okay|go ahead|go ahead and|now)\s+commit(?:\s+(?:my|the|these|all|current))?(?:\s+changes?)?\s*$`),
}

// Negative patterns — if any matches, do NOT trigger the builtin.
var commitNegatives = []*regexp.Regexp{
	// Action words that indicate something other than creating a commit.
	regexp.MustCompile(`\b(?:explain|describe|show|list|view|revert|undo|amend|squash|cherry.?pick|reset|bisect)\b`),
	// References to commit metadata/history.
	regexp.MustCompile(`\b(?:last|previous|recent|latest)\s+commit`),
	regexp.MustCompile(`\bcommit\s+(?:hash|id|sha|message|log|history|diff)\b`),
	// Git log/status queries.
	regexp.MustCompile(`\bgit\s+(?:log|status|diff|show|blame)\b`),
	// Multi-clause sentences — too complex for safe intent detection.
	regexp.MustCompile(`\b(?:and also|and then|but also|then also)\b`),
}

func matchCommitIntent(lower string) *IntentMatch {
	// Check negatives first.
	for _, neg := range commitNegatives {
		if neg.MatchString(lower) {
			return nil
		}
	}

	for _, pat := range commitPatterns {
		matches := pat.FindStringSubmatch(lower)
		if matches == nil {
			continue
		}

		// Extract hint from capture group if present.
		hint := ""
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				hint = strings.TrimSpace(matches[i])
				break
			}
		}

		return &IntentMatch{
			Operation: "commit",
			Args:      hint,
		}
	}

	return nil
}

func isQuestion(s string) bool {
	if strings.HasSuffix(s, "?") {
		return true
	}
	questionPrefixes := []string{
		"what ", "how ", "why ", "when ", "which ", "where ", "who ",
		"can you explain", "tell me about", "do you know",
		"is there", "are there", "does ", "did ",
	}
	for _, p := range questionPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
