package qacache

import (
	"regexp"
	"strings"
	"time"
)

// isoDateRe matches YYYY-MM-DD anywhere in the question; presence
// indicates time intent regardless of relative-time keywords.
var isoDateRe = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// QuestionClass categorizes questions by the volatility of their answers,
// which sets the cache TTL. The classifier is intentionally conservative:
// when in doubt, classify as the shorter-TTL bucket so we err on the side
// of re-deriving rather than serving stale.
type QuestionClass string

const (
	// ClassTimeRelative covers "today / yesterday / this week" style.
	// Answers go stale fast because git/filesystem/state changes constantly.
	ClassTimeRelative QuestionClass = "time_relative"
	// ClassReference covers "what's our build command / what does this
	// function do" — answers track repo state and turn over more slowly.
	ClassReference QuestionClass = "reference"
	// ClassDecision covers "why did we / what did we decide about X" —
	// answers about past choices that, once correct, stay correct.
	ClassDecision QuestionClass = "decision"
	// ClassUnknown is the catch-all when no signal hits. Treated as
	// reference-tier TTL.
	ClassUnknown QuestionClass = "unknown"
)

// TTL returns the cache TTL for a question class. Values come from the
// memory plan: time-relative 2h, reference 7d, decision 30d.
func (c QuestionClass) TTL() time.Duration {
	switch c {
	case ClassTimeRelative:
		return 2 * time.Hour
	case ClassReference:
		return 7 * 24 * time.Hour
	case ClassDecision:
		return 30 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// Classify inspects a raw question and returns its class. Uses keyword
// presence rather than NLP — cheap and good enough for the initial cut.
// False negatives are safe (defaults to reference TTL); false positives
// on the time-relative bucket are also safe (short TTL).
//
// Classify must be called with the pre-normalization text so relative-
// time tokens like "today" / "this week" survive the lookup. After
// Normalize replaces them with absolute dates, the time-marker keywords
// no longer match.
func Classify(question string) QuestionClass {
	q := strings.ToLower(question)
	if hasAny(q, timeMarkers) || isoDateRe.MatchString(q) {
		return ClassTimeRelative
	}
	if hasAny(q, decisionMarkers) {
		return ClassDecision
	}
	if hasAny(q, referenceMarkers) {
		return ClassReference
	}
	return ClassUnknown
}

// Keyword sets, intentionally short and high-signal. Expand as the
// telemetry from /qacache stats accumulates.
var (
	timeMarkers = []string{
		// Date words.
		"today", "yesterday", "tomorrow",
		// Window words.
		"this week", "last week", "next week",
		"this month", "last month", "next month",
		"this year", "last year",
		// Recency adverbs.
		"recent", "recently", "just now", "lately",
	}
	referenceMarkers = []string{
		// Capability / configuration / how-to.
		"how do i", "how to", "where is", "where do",
		"what is", "what's", "what does",
		"how does", "which file", "which command",
		"build command", "test command", "run command",
		"config", "configuration", "settings",
	}
	decisionMarkers = []string{
		// Rationale / history of decisions.
		"why did", "why do we", "why are we",
		"decided", "decision", "trade-off", "tradeoff",
		"reasoning", "rationale",
	}
)

func hasAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
