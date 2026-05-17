package detector

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// qualifyingRules are matched against the normalized error message.
// Each entry: a substring or regex pattern → category. First match
// wins. The disqualify list runs first; anything matching there is
// dropped before the qualify pass.
//
// Conservative bias: when in doubt, do NOT qualify. False positives
// here trigger expensive autoloop runs in later phases; false
// negatives are merely missed self-heal opportunities.
type rule struct {
	rx       *regexp.Regexp // nil → use substr
	substr   string         // case-insensitive substring
	category Category
}

// Disqualifying patterns — these come from real ycode error surfaces
// that look like errors but are normal program behavior or transient
// environment issues. The carrier-program-exited-non-zero case is the
// one we just fixed in commit 2ba0813 (lsof no-match, grep no-match).
var disqualifyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bexit status [1-9][0-9]*\b`), // carrier program returned non-zero — covered by 2ba0813
	regexp.MustCompile(`(?i)context deadline exceeded`),   // transient network/timeout
	regexp.MustCompile(`(?i)connection refused`),          // remote service down — not a ycode bug
	regexp.MustCompile(`(?i)i/o timeout`),
	regexp.MustCompile(`(?i)no such host`),
	regexp.MustCompile(`(?i)broken pipe`),
	regexp.MustCompile(`(?i)signal: (interrupt|killed|terminated)`),     // operator cancellation
	regexp.MustCompile(`(?i)permission denied`),                         // typically filesystem perms — user environment
	regexp.MustCompile(`(?i)json: cannot unmarshal .* into .* of type`), // user-supplied bad JSON
	regexp.MustCompile(`(?i)invalid (regex|regular expression)`),        // user-supplied bad regex
	regexp.MustCompile(`(?i)file (does not exist|not found)`),           // user-supplied path
}

// Qualifying patterns. Order matters within categories — most specific first.
var qualifyRules = []rule{
	// Broken — ycode's own code raised an error
	{rx: regexp.MustCompile(`(?i)\bpanic\b`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)nil pointer dereference`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)marshal: unsupported type`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)cannot marshal`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)schema validation`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)\bunknown tool\b`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)\bunknown method\b`), category: CategoryBroken},
	{rx: regexp.MustCompile(`(?i)\bunhandled\b`), category: CategoryBroken},
	// MCP JSON-RPC method-not-found / invalid-params codes
	{substr: "-32601", category: CategoryBroken},
	{substr: "-32602", category: CategoryBroken},

	// Missing — feature surface advertises support that isn't wired
	{rx: regexp.MustCompile(`(?i)not (yet )?implemented`), category: CategoryMissing},
	{rx: regexp.MustCompile(`(?i)action \S+ not supported`), category: CategoryMissing},
	{rx: regexp.MustCompile(`(?i)\bnot supported\b`), category: CategoryMissing},
	{rx: regexp.MustCompile(`(?i)\breturns? an unsupported\b`), category: CategoryMissing},
	{rx: regexp.MustCompile(`(?i)\brequires\b.*\bmode\b`), category: CategoryMissing}, // "requires browser.mode" style hints
}

// Classifier turns raw error messages into a qualification verdict and
// a stable signature. Holds no state — safe to share across detector
// goroutines.
type Classifier struct{}

// Qualify reports whether a (tool, scope, raw error) triple looks like
// a ycode-bug-shaped failure. Returns the category, the normalized
// error string used for the signature, and the signature hex prefix.
func (c *Classifier) Qualify(tool, scope, rawErr string) (cat Category, normalized, signature string, qualifies bool) {
	if rawErr == "" {
		return "", "", "", false
	}
	normalized = normalizeError(rawErr)
	for _, rx := range disqualifyPatterns {
		if rx.MatchString(normalized) {
			return "", normalized, "", false
		}
	}
	for _, r := range qualifyRules {
		if r.rx != nil {
			if r.rx.MatchString(normalized) {
				return r.category, normalized, makeSignature(tool, string(r.category), normalized), true
			}
			continue
		}
		if r.substr != "" && strings.Contains(strings.ToLower(normalized), strings.ToLower(r.substr)) {
			return r.category, normalized, makeSignature(tool, string(r.category), normalized), true
		}
	}
	return "", normalized, "", false
}

// makeSignature is sha256(tool|category|normalized)[:12] — short
// enough for filenames/branch names, long enough for collision
// resistance across an operator's lifetime of fixes.
func makeSignature(tool, cat, normalized string) string {
	h := sha256.Sum256([]byte(tool + "|" + cat + "|" + normalized))
	return hex.EncodeToString(h[:6]) // 12 hex chars
}

// Normalization patterns: each one collapses a high-cardinality
// fragment into a placeholder so semantically-identical errors hash
// to the same signature regardless of incidental detail. Order
// matters — apply hex/UUID/timestamp before path so digits inside
// paths don't accidentally become <L> first.
var normalizers = []struct {
	rx   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)0x[0-9a-f]{4,}`), "<ADDR>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?\b`), "<TS>"},
	{regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<UUID>"},
	// Absolute paths: any whitespace-delimited token that starts with
	// `/` AND contains a second `/` collapses to <PATH>. Aggressive on
	// purpose — every distinct operator path on disk would otherwise
	// give a distinct signature, defeating dedupe. Tokens like `/`
	// alone or `/foo` (single segment) are left intact since they're
	// rarely path-shaped in practice.
	{regexp.MustCompile(`/[^/\s:]+(?:/[^\s:]+)+`), "<PATH>"},
	{regexp.MustCompile(`:\d+:\d+\b`), ":<L>:<C>"}, // file:line:col
	{regexp.MustCompile(`:\d{2,}\b`), ":<L>"},      // file:line
	{regexp.MustCompile(`\bpid[ =]?\d+\b`), "pid=<N>"},
	{regexp.MustCompile(`\bport[ =]?\d{2,}\b`), "port=<N>"},
	// Unify loopback hosts: 127.0.0.1 and localhost are
	// interchangeable in practice and should hash equally.
	{regexp.MustCompile(`\b(127\.0\.0\.1|::1|localhost)\b`), "<LOCALHOST>"},
	{regexp.MustCompile(`<LOCALHOST>:\d+\b`), "<LOCALHOST>:<PORT>"},
	{regexp.MustCompile(`\b[0-9a-f]{16,}\b`), "<HEX>"},
	{regexp.MustCompile(`\s+`), " "}, // collapse runs of whitespace last
}

// normalizeError applies every normalizer in order. The result is a
// canonical form for signature hashing AND a readable artifact for
// the JSONL log.
func normalizeError(s string) string {
	s = strings.TrimSpace(s)
	for _, n := range normalizers {
		s = n.rx.ReplaceAllString(s, n.with)
	}
	return strings.TrimSpace(s)
}
