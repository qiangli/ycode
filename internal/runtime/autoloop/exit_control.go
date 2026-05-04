package autoloop

import (
	"regexp"
	"strings"
)

// ExitControl implements dual-layer exit detection and question-loop suppression
// for autonomous loops. Inspired by ralph-claude-code's EXIT_SIGNAL protocol
// and question detection system.
//
// Dual-layer exit: the loop exits only when BOTH heuristic completion indicators
// AND an explicit exit signal are present. This prevents false positives from
// documentation text that mentions "complete" or "done".
//
// Question suppression: detects when the agent asks questions instead of acting
// in headless/autonomous mode, and provides corrective guidance.

// ExitSignal represents the agent's explicit intent to exit.
type ExitSignal struct {
	// Detected is true if the agent explicitly signaled exit.
	Detected bool
	// WorkType classifies what the agent was doing (implementation, testing, etc.)
	WorkType string
}

var questionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bshould I\b`),
	regexp.MustCompile(`(?i)\bdo you want\b`),
	regexp.MustCompile(`(?i)\bwould you like\b`),
	regexp.MustCompile(`(?i)\bwhich option\b`),
	regexp.MustCompile(`(?i)\bcan you clarify\b`),
	regexp.MustCompile(`(?i)\bwhat would you prefer\b`),
	regexp.MustCompile(`(?i)\bshall I\b`),
	regexp.MustCompile(`(?i)\bdo you prefer\b`),
	regexp.MustCompile(`(?i)\bplease confirm\b`),
}

var completionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\ball tasks? (?:are |have been )?completed?\b`),
	regexp.MustCompile(`(?i)\bwork is (?:done|complete|finished)\b`),
	regexp.MustCompile(`(?i)\bno (?:remaining|more|further) (?:tasks?|work|gaps?)\b`),
	regexp.MustCompile(`(?i)\bsuccessfully (?:completed|finished|implemented)\b`),
}

// DetectQuestions checks if the response contains question patterns indicating
// the agent is asking for input instead of acting autonomously.
// Returns the count of distinct question patterns found.
func DetectQuestions(response string) int {
	count := 0
	for _, re := range questionPatterns {
		if re.MatchString(response) {
			count++
		}
	}
	return count
}

// IsAskingQuestions returns true if the response has 2+ question patterns,
// suggesting the agent is blocked waiting for user input.
func IsAskingQuestions(response string) bool {
	return DetectQuestions(response) >= 2
}

// QuestionSuppressionMessage returns the corrective guidance to inject
// when the agent asks questions in autonomous mode.
func QuestionSuppressionMessage() string {
	return "You asked questions in the previous iteration. This is autonomous mode — " +
		"there is no human to answer. Choose the most conservative default and proceed. " +
		"If you are genuinely blocked, signal EXIT_SIGNAL: true with a summary of what blocked you."
}

// DetectCompletionIndicators counts how many completion patterns appear in the response.
func DetectCompletionIndicators(response string) int {
	count := 0
	for _, re := range completionPatterns {
		if re.MatchString(response) {
			count++
		}
	}
	return count
}

// ParseExitSignal extracts the EXIT_SIGNAL from a structured status block in
// the agent's response. Expected format:
//
//	STATUS: COMPLETE
//	EXIT_SIGNAL: true
func ParseExitSignal(response string) ExitSignal {
	signal := ExitSignal{}
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "EXIT_SIGNAL:"); ok {
			signal.Detected = strings.TrimSpace(after) == "true"
		}
		if after, ok := strings.CutPrefix(trimmed, "WORK_TYPE:"); ok {
			signal.WorkType = strings.TrimSpace(after)
		}
	}
	return signal
}

// ShouldExit implements the dual-layer exit check.
// Returns true only when BOTH conditions are met:
// 1. At least minIndicators completion indicators are present.
// 2. The agent explicitly signaled EXIT_SIGNAL: true.
func ShouldExit(response string, minIndicators int) bool {
	if minIndicators <= 0 {
		minIndicators = 2
	}
	indicators := DetectCompletionIndicators(response)
	signal := ParseExitSignal(response)
	return indicators >= minIndicators && signal.Detected
}
