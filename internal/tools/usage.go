package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Skill-usage telemetry — one JSON line per /<skill> invocation, appended
// to ~/.agents/ycode/skill-usage.jsonl. Designed for a "dogfood the
// catalog" workflow: collect a week of real invocations and analyse with
// jq at the end.
//
// Failure-quiet by design: a telemetry write that fails (no home dir,
// disk full, permission denied) MUST NOT break the skill dispatch. The
// caller's return value is unaffected.
//
// File format: one JSON object per line, schema below. Append-only; no
// rotation. A week of personal use is kilobytes.

// usageEvent is the per-invocation record written to skill-usage.jsonl.
type usageEvent struct {
	Ts        string `json:"ts"`                 // RFC3339 UTC
	Name      string `json:"name"`               // skill name as invoked (pre-normalisation)
	Source    string `json:"source"`             // see usageSource* constants
	ArgsLen   int    `json:"args_len"`           // byte length of args (we don't log args themselves)
	Ok        bool   `json:"ok"`                 // dispatch succeeded
	ErrKind   string `json:"err_kind,omitempty"` // classification when !Ok
	LatencyMs int64  `json:"latency_ms"`         // wall time across the dispatch
}

// Source classifications recorded in the JSONL — these are the tags used
// for end-of-week aggregation.
const (
	usageSourceInternal        = "internal"         // local overlay (.agents/ycode/skills/, $YCODE_SKILLS_DIR, …)
	usageSourceExternal        = "external"         // dhnt catalog body returned to LLM
	usageSourceExternalBuiltin = "external_builtin" // catalog entry routed to a registered builtin executor
	usageSourceExternalCNL     = "external_cnl"     // catalog entry with executor=cnl (not yet supported)
	usageSourceBuiltin         = "builtin"          // legacy builtin without a catalog entry
	usageSourceNotFound        = "not_found"        // no skill matched
)

// usageDir is overridable for tests.
var usageDir = func() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".agents", "ycode"), true
}

var usageMu sync.Mutex

// recordSkillUsage appends one event to the usage log. Never panics;
// silent on any error.
func recordSkillUsage(name string, argsLen int, source string, err error, latency time.Duration) {
	dir, ok := usageDir()
	if !ok {
		return
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return
	}
	ev := usageEvent{
		Ts:        time.Now().UTC().Format(time.RFC3339),
		Name:      name,
		Source:    source,
		ArgsLen:   argsLen,
		Ok:        err == nil,
		ErrKind:   classifyErr(err),
		LatencyMs: latency.Milliseconds(),
	}
	line, mErr := json.Marshal(&ev)
	if mErr != nil {
		return
	}
	line = append(line, '\n')

	path := filepath.Join(dir, "skill-usage.jsonl")

	usageMu.Lock()
	defer usageMu.Unlock()
	f, oErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if oErr != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(line)
}

// classifyErr maps a dispatch error into a coarse bucket for analysis.
// The full error string is intentionally not logged — it may contain
// args or paths the user doesn't want in a usage log.
func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return "not_found"
	case strings.Contains(msg, "executor=cnl"):
		return "cnl_unsupported"
	default:
		return "executor_error"
	}
}
