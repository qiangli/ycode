// Package backlogsink synthesizes a docs/backlog/<slug>.md entry from
// each first-seen FailureSignal. Phase 2 of the selfheal plan
// (/Users/qiangli/.claude/plans/summarize-the-previous-issues-squishy-cupcake.md).
//
// Reuses the existing backlog primitive at internal/gitserver/backlog
// so the synthesized entries flow through the same Foreman/Worker
// dispatch path as human-authored items. The Phase 3 worker reads
// these entries to learn what to fix.
//
// Kept in its own package (not under detector/) so the detector stays
// free of Gitea/backlog imports — useful for cheap unit tests and for
// any future caller that wants the observer without the
// backlog-synthesis side effect.
package backlogsink

import (
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/gitserver/backlog"
	"github.com/qiangli/ycode/internal/runtime/selfheal/detector"
)

// BacklogSink is a detector.Sink that writes a markdown backlog entry
// for the first sighting of each signature. Subsequent sightings
// within the dedupe window never reach here (the detector suppresses
// them) but if one did, the sink's idempotent skip-if-exists guard
// prevents accidental overwrites.
//
// The frontmatter schema is the canonical one
// (internal/gitserver/backlog.Issue) — no extension. Selfheal-specific
// context (signature, trigger category, occurrence count, raw error)
// lives in a fenced YAML block in the body so the Phase 3 worker can
// parse it back without schema changes.
type BacklogSink struct {
	dir      string                    // resolved per-project backlog dir
	priority string                    // default p2 — auto-discovered fixes never claim p1
	writer   func(backlog.Issue) error // injectable for tests
}

// NewBacklogSink returns a sink that writes to dir. Dir must already
// exist — caller (cmd/ycode/otel.go) resolves it via projectid
// helpers; the sink stays platform-agnostic.
func NewBacklogSink(dir string) *BacklogSink {
	return &BacklogSink{
		dir:      dir,
		priority: backlog.PriorityP2,
		writer:   defaultWriter(dir),
	}
}

// Record produces one backlog entry. Idempotent: if the entry already
// exists (same signature, same slug), the write is skipped without
// error so Phase 1 → 2 transitions don't blow up on signatures already
// captured in a previous serve.
func (s *BacklogSink) Record(sig detector.FailureSignal) error {
	slug := buildSlug(sig)
	issue := backlog.Issue{
		Slug:     slug,
		Title:    buildTitle(sig),
		Priority: s.priority,
		State:    backlog.StateOpen,
		Created:  sig.Timestamp,
		Body:     buildBody(sig),
	}
	if err := s.writer(issue); err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

// Close is a no-op — the sink keeps no resources. Satisfies the
// detector.Sink interface.
func (s *BacklogSink) Close() error { return nil }

// buildSlug encodes the signature in the filename so worker tooling
// can map (slug ↔ signature) deterministically without a sidecar
// index. Format: selfheal-<sig12>-<short-tool-slug>.
func buildSlug(sig detector.FailureSignal) string {
	toolSlug := sanitizeForSlug(sig.ToolName)
	if toolSlug == "" {
		toolSlug = "unknown"
	}
	return fmt.Sprintf("selfheal-%s-%s", sig.Signature, toolSlug)
}

func buildTitle(sig detector.FailureSignal) string {
	shortErr := truncate(sig.Normalized, 60)
	return fmt.Sprintf("selfheal: %s: %s", sig.ToolName, shortErr)
}

// buildBody renders the failure context as fenced YAML inside the
// markdown body. Keeping it fenced means humans reading the file see
// a readable block and the worker's parser has unambiguous
// boundaries.
func buildBody(sig detector.FailureSignal) string {
	var b strings.Builder
	b.WriteString("Auto-discovered failure signal. Source: ycode selfheal observer (Phase 2).\n\n")
	b.WriteString("```yaml\n")
	b.WriteString("source: selfheal\n")
	fmt.Fprintf(&b, "signature: %s\n", sig.Signature)
	fmt.Fprintf(&b, "category: %s\n", sig.Category)
	fmt.Fprintf(&b, "tool: %s\n", sig.ToolName)
	fmt.Fprintf(&b, "scope: %s\n", sig.Scope)
	fmt.Fprintf(&b, "first_seen: %s\n", sig.Timestamp.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "occurrence_count: %d\n", sig.OccurrenceN)
	if sig.ExitClass != "" {
		fmt.Fprintf(&b, "exit_class: %s\n", sig.ExitClass)
	}
	if sig.AgentClient != "" {
		fmt.Fprintf(&b, "agent_client: %s\n", sig.AgentClient)
	}
	if sig.WrapAgent != "" {
		fmt.Fprintf(&b, "wrap_agent: %s\n", sig.WrapAgent)
	}
	if sig.DurationMs > 0 {
		fmt.Fprintf(&b, "duration_ms: %d\n", sig.DurationMs)
	}
	b.WriteString("```\n\n")
	b.WriteString("### Normalized error\n\n")
	b.WriteString("```\n")
	b.WriteString(sig.Normalized)
	b.WriteString("\n```\n\n")
	if sig.ErrorMessage != "" && sig.ErrorMessage != sig.Normalized {
		b.WriteString("### Raw error (pre-normalization, sanitization may still be needed before sharing)\n\n")
		b.WriteString("```\n")
		b.WriteString(sig.ErrorMessage)
		b.WriteString("\n```\n")
	}
	return b.String()
}

// sanitizeForSlug constrains a tool name to the kebab-case alphabet
// used elsewhere by cmd/ycode/backlog.go:slugify. Duplicating the
// trivial filter rather than importing cmd/main keeps the dependency
// direction clean (internal → main is disallowed in Go).
func sanitizeForSlug(s string) string {
	var b strings.Builder
	s = strings.ToLower(strings.TrimSpace(s))
	prevDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 32 {
		out = strings.TrimRight(out[:32], "-")
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
