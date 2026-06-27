// Package qacache stores the agent's prior answers keyed by a normalized
// form of the question so a repeated ask returns instantly from cache
// rather than re-deriving from raw sources (git log, file search, etc.).
//
// The cache sits in front of the model call as a context injector: a
// hit adds a <recent-answer> block to the prompt; the LLM still runs.
// This is the "inject as context, not short-circuit" policy chosen for
// the memory plan — see /Users/you/.claude/plans/best-in-class-memory-snazzy-cocoa.md
// for the rationale.
package qacache

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// punctuation we strip on normalization. Apostrophes are kept so words
// like "don't" survive as one token.
var punctRe = regexp.MustCompile(`[^\w\s'-]+`)

// whitespace collapse.
var wsRe = regexp.MustCompile(`\s+`)

// time-relative token detector.
var relativeTimeTokens = map[string]int{
	// label → day offset relative to "now". 0 means today.
	"today":     0,
	"yesterday": -1,
	"tomorrow":  1,
}

// NormalizedQuestion is the key produced by the normalizer plus the
// rich form used for similarity lookups.
type NormalizedQuestion struct {
	// Key is the SHA-256 hex prefix used as the primary cache key.
	Key string
	// Canonical is the normalized text — lowercased, punctuation-stripped,
	// whitespace-collapsed, with relative-time tokens replaced by absolute
	// ISO dates per the reference time. Stable across runs when "now" is
	// the same day.
	Canonical string
	// DateTokens lists the absolute YYYY-MM-DD values produced by
	// relative-time resolution. Empty when the question has no time
	// intent. Used for ±1-day fuzzy match across midnight.
	DateTokens []string
}

// Normalize turns a free-text question into a deterministic key for the
// cache. Relative-time tokens like "today" and "yesterday" are resolved
// to absolute dates at write time so two queries asked on the same day
// produce the same key and a query asked the next day naturally misses
// (we want a fresh answer when reality may have moved).
func Normalize(question string, now time.Time) NormalizedQuestion {
	q := strings.ToLower(question)
	q = punctRe.ReplaceAllString(q, " ")
	q = wsRe.ReplaceAllString(q, " ")
	q = strings.TrimSpace(q)

	// Resolve relative-time tokens to absolute dates.
	tokens := strings.Split(q, " ")
	var dateTokens []string
	for i, tok := range tokens {
		if delta, ok := relativeTimeTokens[tok]; ok {
			d := now.AddDate(0, 0, delta).Format("2006-01-02")
			tokens[i] = d
			dateTokens = append(dateTokens, d)
		}
	}
	canonical := strings.Join(tokens, " ")

	sum := sha256.Sum256([]byte(canonical))
	return NormalizedQuestion{
		Key:        hex.EncodeToString(sum[:16]),
		Canonical:  canonical,
		DateTokens: dateTokens,
	}
}
