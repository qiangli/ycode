package qacache

import (
	"fmt"
	"strings"
	"time"
)

// Injector wraps a Cache with formatting suitable for prompt injection.
// It's a thin facade: the runtime owns the conversation loop and calls
// Lookup before sending the request, then Record after the response
// comes back. Both methods are nil-safe — when the injector or its
// cache is unset, they return empty/no-op.
//
// Format policy follows the memory plan: a hit renders as a
// <recent-answer> block. We never short-circuit the LLM call — the LLM
// still runs and can decide to reuse or refine the cached answer. This
// is the "inject as context, not short-circuit" choice (Phase 2 design,
// user-confirmed).
type Injector struct {
	cache *Cache
}

// NewInjector returns an Injector. Cache may be nil; the injector then
// becomes a no-op.
func NewInjector(c *Cache) *Injector { return &Injector{cache: c} }

// Lookup returns a formatted <recent-answer> block for injection into
// the system prompt, or empty string when there is no cached answer for
// the question. The match also bumps AskCount on the underlying entry,
// which feeds the promotion path.
func (i *Injector) Lookup(question string, now time.Time) string {
	if i == nil || i.cache == nil {
		return ""
	}
	e := i.cache.Lookup(question, now)
	if e == nil {
		return ""
	}
	return formatRecentAnswer(e)
}

// Record stores the question/answer pair so subsequent asks hit the
// cache. Entities feed entity-based invalidation; sources are recorded
// for re-derivation hints. now should be the time the answer was
// produced (typically time.Now() at end-of-turn).
func (i *Injector) Record(question, answer string, entities, sources []string, now time.Time) {
	if i == nil || i.cache == nil {
		return
	}
	if strings.TrimSpace(question) == "" || strings.TrimSpace(answer) == "" {
		return
	}
	i.cache.Record(question, answer, now, entities, sources)
}

// Cache exposes the underlying cache for callers that need direct
// access (e.g., the /qacache stats builtin or the Dreamer promotion
// pass). Returns nil when the injector was constructed with no cache.
func (i *Injector) Cache() *Cache {
	if i == nil {
		return nil
	}
	return i.cache
}

// formatRecentAnswer renders a single entry as a <recent-answer> block
// suitable for inclusion in the system prompt's runtime-diagnostics
// section. Keeps the format tight; the agent re-derives if more detail
// is needed.
func formatRecentAnswer(e *Entry) string {
	if e == nil {
		return ""
	}
	freshness := freshnessLabel(e)
	var b strings.Builder
	fmt.Fprintf(&b, "<recent-answer freshness=%q asked=%d class=%q>\n", freshness, e.AskCount, string(e.Class))
	fmt.Fprintf(&b, "Q: %s\n", e.Question)
	fmt.Fprintf(&b, "A: %s\n", e.Answer)
	if len(e.Sources) > 0 {
		fmt.Fprintf(&b, "(sources: %s)\n", strings.Join(e.Sources, ", "))
	}
	b.WriteString("</recent-answer>")
	return b.String()
}

func freshnessLabel(e *Entry) string {
	if e == nil || e.CreatedAt.IsZero() {
		return "unknown"
	}
	age := time.Since(e.CreatedAt)
	switch {
	case age < time.Hour:
		return "minutes-old"
	case age < 24*time.Hour:
		return "hours-old"
	case age < 7*24*time.Hour:
		return "days-old"
	default:
		return "weeks-old"
	}
}
