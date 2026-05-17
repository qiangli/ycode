// Package detector watches OTel tool-call spans for failures that look
// like ycode bugs and emits structured observations. Phase 1 of the
// selfheal system (see plan
// /Users/qiangli/.claude/plans/summarize-the-previous-issues-squishy-cupcake.md):
// observe-only. No backlog synthesis, no worker dispatch, no git.
//
// The detector runs as an OTel sdktrace.SpanProcessor registered on
// the global TracerProvider, so it sees every span ycode emits
// (ycode.tool.call, ycode.exec.*, ycode.browser.*, ycode.inference.*,
// ycode.search.*) without per-call-site plumbing.
package detector

import "time"

// Category enumerates the reasons a failing span qualifies for
// follow-up. Phase 1 ships broken + missing; perf and reformulation
// land in later phases (perf needs a sliding-window baseline,
// reformulation needs cross-span correlation).
type Category string

const (
	CategoryBroken  Category = "broken"  // a ycode-internal error: panic, unknown method, marshal failure, …
	CategoryMissing Category = "missing" // a tool advertises support that isn't wired (e.g. live mode eval pre-a8a74f3)
)

// FailureSignal is the post-classification observation written to the
// JSONL sink. One signal per qualifying span. Keeping the shape stable
// matters: Phase 2 will read these back to synthesize backlog entries.
type FailureSignal struct {
	Timestamp    time.Time `json:"ts"`
	Signature    string    `json:"signature"`
	Category     Category  `json:"category"`
	ToolName     string    `json:"tool"`
	Scope        string    `json:"scope"`      // span name family: ycode.tool.call, ycode.exec.bash, …
	ErrorMessage string    `json:"error"`      // raw message before normalization (truncated)
	Normalized   string    `json:"normalized"` // normalized form fed into the signature hash
	ExitClass    string    `json:"exit_class,omitempty"`
	DurationMs   int64     `json:"duration_ms"`
	AgentClient  string    `json:"agent_client,omitempty"`
	WrapAgent    string    `json:"wrap_agent,omitempty"`
	OccurrenceN  int       `json:"occurrence_n"` // 1 on first sighting, ++ on dedupe hits
}

// rawSpan is the minimal projection of an OTel ReadOnlySpan the
// detector cares about. Keeping the SpanProcessor's translation
// surface narrow makes the classifier easy to unit-test without
// pulling in the SDK.
type rawSpan struct {
	Name        string
	StartTime   time.Time
	EndTime     time.Time
	StatusError string            // empty when status is Ok
	Attributes  map[string]string // flattened key→string; only the keys the detector reads
}

// errMaxLen caps how much of the error message ends up in the JSONL
// log. Real ycode errors are usually short; truncating at 1 KiB
// guards against accidental log explosion when something wraps a
// large payload into err.Error().
const errMaxLen = 1024

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
