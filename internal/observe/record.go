// Package observe records a structured, per-turn action record for an agent
// run: for every turn it captures the outbound REQUEST (served model, base
// model, escalation, provider, params, prompt token count + hash), the inbound
// RESPONSE (finish reason, completion, usage, locally-priced cost), and EVERY
// tool call (name, arguments, result/error, status, duration). At session end
// it emits a SUMMARY with per-model and per-tool totals.
//
// The record is written as JSONL (one object per line) and, when an OTEL
// tracer is wired, also emitted as an `agent.turn` span with tool-call events.
// This is the client-side, local-first observability layer from the design of
// record (docs/agent-run-observability-otel.md): the code that BUILDS the
// request and READS the response knows everything — served model, tokens, and
// cost (tokens × a local price table) — with no gateway or billing API in the
// loop.
//
// Redaction is a first-class concern: prompts are stored as a hash by default
// (full text only under --trace-verbose), and vault secrets / API keys are
// scrubbed from every free-text field before it is written. See redact.go.
package observe

import "time"

// Record type discriminators, written as the JSON "type" field so a reader can
// tell turn records from the trailing session summary.
const (
	KindTurn    = "turn"
	KindSummary = "summary"

	// StatusOK / StatusError classify a single tool call.
	StatusOK    = "ok"
	StatusError = "error"
)

// RequestRecord is the outbound side of one turn — what the client put on the
// wire. served_model is the model the cascade ACTUALLY used this turn; base_model
// is the configured base; escalated is true when the two differ (the direct,
// per-turn premium-intervention signal that replaces log-grepping).
type RequestRecord struct {
	ServedModel     string   `json:"served_model"`
	BaseModel       string   `json:"base_model"`
	Escalated       bool     `json:"escalated"`
	Reason          string   `json:"reason,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	// PromptTokens is the billed prompt size for the turn. Authoritative value
	// comes from the response usage; it is backfilled onto the request record.
	PromptTokens int `json:"prompt_tokens"`
	// PromptHash is a stable reference to the prompt (never the prompt itself),
	// so turns are comparable without dumping the transcript.
	PromptHash string `json:"prompt_hash"`
	// Prompt is the full prompt text, populated ONLY when the recorder runs in
	// verbose mode (--trace-verbose). Redacted even then.
	Prompt string `json:"prompt,omitempty"`
}

// ToolCallRecord captures one tool invocation within a turn: name + arguments +
// result/error + status + duration, exactly what the gate requires to see a
// tool-failure loop directly.
type ToolCallRecord struct {
	Name       string `json:"name"`
	Arguments  string `json:"arguments,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
}

// ResponseRecord is the inbound side of one turn.
type ResponseRecord struct {
	FinishReason string `json:"finish_reason,omitempty"`
	// Completion is the model's text output (redacted, and truncated unless
	// verbose). CompletionHash is always a full-text reference.
	Completion       string  `json:"completion,omitempty"`
	CompletionHash   string  `json:"completion_hash,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd"`
}

// TurnRecord is the full per-turn action record — one JSONL line per turn.
type TurnRecord struct {
	Kind       string           `json:"type"`
	SessionID  string           `json:"session_id"`
	Turn       int              `json:"turn"`
	StartedAt  time.Time        `json:"started_at"`
	EndedAt    time.Time        `json:"ended_at"`
	DurationMs int64            `json:"duration_ms"`
	Request    RequestRecord    `json:"request"`
	Response   ResponseRecord   `json:"response"`
	ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
}

// ModelTotals aggregates per-served-model usage over a session.
type ModelTotals struct {
	Turns            int     `json:"turns"`
	Escalations      int     `json:"escalations"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// ToolTotals aggregates per-tool call counts and failures over a session.
type ToolTotals struct {
	Calls    int `json:"calls"`
	Failures int `json:"failures"`
}

// Summary is the session-end reconciliation record — one JSONL line, written
// last. Its totals are aggregated from the exact per-turn records as they are
// flushed, so summary always reconciles with the sum of the turns.
type Summary struct {
	Kind             string                  `json:"type"`
	SessionID        string                  `json:"session_id"`
	Turns            int                     `json:"turns"`
	Escalations      int                     `json:"escalations"`
	PromptTokens     int                     `json:"prompt_tokens"`
	CompletionTokens int                     `json:"completion_tokens"`
	ToolCalls        int                     `json:"tool_calls"`
	ToolFailures     int                     `json:"tool_failures"`
	CostUSD          float64                 `json:"cost_usd"`
	PerModel         map[string]*ModelTotals `json:"per_model"`
	PerTool          map[string]*ToolTotals  `json:"per_tool"`
	StartedAt        time.Time               `json:"started_at"`
	EndedAt          time.Time               `json:"ended_at"`
}

// ToolFailureRate returns failed tool calls / total tool calls in [0,1].
func (s *Summary) ToolFailureRate() float64 {
	if s == nil || s.ToolCalls == 0 {
		return 0
	}
	return float64(s.ToolFailures) / float64(s.ToolCalls)
}
