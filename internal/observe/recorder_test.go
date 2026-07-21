package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// decode splits a JSONL buffer into per-line generic maps plus the typed turn
// and summary records, for assertions.
func decodeLines(t *testing.T, buf *bytes.Buffer) (turns []TurnRecord, summary *Summary) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("line is not valid JSON: %v\n%s", err, line)
		}
		switch probe.Type {
		case KindTurn:
			var tr TurnRecord
			if err := json.Unmarshal([]byte(line), &tr); err != nil {
				t.Fatalf("bad turn record: %v", err)
			}
			turns = append(turns, tr)
		case KindSummary:
			var s Summary
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				t.Fatalf("bad summary record: %v", err)
			}
			summary = &s
		default:
			t.Fatalf("unknown record type %q", probe.Type)
		}
	}
	return turns, summary
}

// flatCost is a deterministic price table for tests: $1/M input, $2/M output.
func flatCost(_ string, prompt, completion, _, _ int) float64 {
	return float64(prompt)*1.0/1_000_000 + float64(completion)*2.0/1_000_000
}

// runCascade drives a small three-turn "cascade" run through the recorder:
// turn 0 base model + a tool call, turn 1 base model, turn 2 escalates to the
// premium model. Returns the buffer.
func runCascade(t *testing.T, opts Options) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	opts.Writer = buf
	if opts.Cost == nil {
		opts.Cost = flatCost
	}
	if opts.SessionID == "" {
		opts.SessionID = "sess-test"
	}
	rec := New(opts)
	ctx := context.Background()

	// Turn 0: base model, one successful + one failed tool call.
	rec.BeginTurn(ctx, BeginParams{Turn: 0, ServedModel: "glm-4.6", BaseModel: "glm-4.6", Provider: "openai", PromptHash: "sha256:abc0"})
	rec.SetResponse(ResponseParams{FinishReason: "tool_use", PromptTokens: 1000, CompletionTokens: 200})
	rec.AddToolCall(ToolParams{Name: "Bash", Arguments: `{"command":"ls"}`, Result: "file.go", DurationMs: 12})
	rec.AddToolCall(ToolParams{Name: "Bash", Arguments: `{"command":"nope"}`, Error: "exit 127", DurationMs: 5})

	// Turn 1: base model, no tools.
	rec.BeginTurn(ctx, BeginParams{Turn: 1, ServedModel: "glm-4.6", BaseModel: "glm-4.6", Provider: "openai", PromptHash: "sha256:abc1"})
	rec.SetResponse(ResponseParams{FinishReason: "tool_use", PromptTokens: 1500, CompletionTokens: 300})
	rec.AddToolCall(ToolParams{Name: "Read", Arguments: `{"file":"x"}`, Result: "ok", DurationMs: 8})

	// Turn 2: escalate to premium model.
	rec.BeginTurn(ctx, BeginParams{Turn: 2, ServedModel: "claude-opus-4-8", BaseModel: "glm-4.6", Reason: "budget_downgrade_reverted", Provider: "anthropic", PromptHash: "sha256:abc2"})
	rec.SetResponse(ResponseParams{FinishReason: "end_turn", PromptTokens: 2000, CompletionTokens: 500, Completion: "done"})

	rec.Finish()
	return buf
}

func TestServedModelAndEscalationPerTurn(t *testing.T) {
	buf := runCascade(t, Options{})
	turns, summary := decodeLines(t, buf)

	if len(turns) != 3 {
		t.Fatalf("want 3 turn records, got %d", len(turns))
	}
	// GATE: served_model present on every turn.
	want := []string{"glm-4.6", "glm-4.6", "claude-opus-4-8"}
	for i, tr := range turns {
		if tr.Request.ServedModel != want[i] {
			t.Errorf("turn %d served_model = %q, want %q", i, tr.Request.ServedModel, want[i])
		}
	}
	// GATE: escalated=true only on the turn that switched to the premium model.
	if turns[0].Request.Escalated || turns[1].Request.Escalated {
		t.Error("base-model turns must not be marked escalated")
	}
	if !turns[2].Request.Escalated {
		t.Error("premium turn must be marked escalated=true")
	}
	if turns[2].Request.Reason == "" {
		t.Error("escalated turn should carry a reason")
	}
	if summary.Escalations != 1 {
		t.Errorf("summary escalations = %d, want 1", summary.Escalations)
	}
}

func TestToolCallsRecorded(t *testing.T) {
	buf := runCascade(t, Options{})
	turns, _ := decodeLines(t, buf)

	// GATE: every tool call appears with name + args + result/error + duration.
	tc := turns[0].ToolCalls
	if len(tc) != 2 {
		t.Fatalf("turn 0 tool calls = %d, want 2", len(tc))
	}
	if tc[0].Name != "Bash" || tc[0].Arguments == "" || tc[0].Result == "" || tc[0].DurationMs == 0 {
		t.Errorf("tool call 0 missing fields: %+v", tc[0])
	}
	if tc[0].Status != StatusOK {
		t.Errorf("tool call 0 status = %q, want ok", tc[0].Status)
	}
	if tc[1].Status != StatusError || tc[1].Error == "" {
		t.Errorf("failed tool call not recorded as error: %+v", tc[1])
	}
}

func TestSummaryReconciles(t *testing.T) {
	buf := runCascade(t, Options{})
	turns, summary := decodeLines(t, buf)
	if summary == nil {
		t.Fatal("no summary record written")
	}

	// GATE: summary totals reconcile with the sum of the per-turn records.
	var wantPrompt, wantCompletion, wantTools, wantFailures int
	var wantCost float64
	perModel := map[string]int{}
	for _, tr := range turns {
		wantPrompt += tr.Response.PromptTokens
		wantCompletion += tr.Response.CompletionTokens
		wantCost += tr.Response.CostUSD
		perModel[tr.Request.ServedModel]++
		for _, c := range tr.ToolCalls {
			wantTools++
			if c.Status == StatusError {
				wantFailures++
			}
		}
	}
	if summary.Turns != len(turns) {
		t.Errorf("summary turns = %d, want %d", summary.Turns, len(turns))
	}
	if summary.PromptTokens != wantPrompt {
		t.Errorf("summary prompt_tokens = %d, want %d", summary.PromptTokens, wantPrompt)
	}
	if summary.CompletionTokens != wantCompletion {
		t.Errorf("summary completion_tokens = %d, want %d", summary.CompletionTokens, wantCompletion)
	}
	if summary.ToolCalls != wantTools {
		t.Errorf("summary tool_calls = %d, want %d", summary.ToolCalls, wantTools)
	}
	if summary.ToolFailures != wantFailures {
		t.Errorf("summary tool_failures = %d, want %d", summary.ToolFailures, wantFailures)
	}
	if !floatEq(summary.CostUSD, wantCost) {
		t.Errorf("summary cost_usd = %v, want %v", summary.CostUSD, wantCost)
	}
	// Per-model totals reconcile too.
	for model, n := range perModel {
		mt := summary.PerModel[model]
		if mt == nil || mt.Turns != n {
			t.Errorf("per_model[%s].turns = %v, want %d", model, mt, n)
		}
	}
	// Per-tool call counts + failure rate.
	if bash := summary.PerTool["Bash"]; bash == nil || bash.Calls != 2 || bash.Failures != 1 {
		t.Errorf("per_tool[Bash] = %+v, want calls=2 failures=1", bash)
	}
	if got := summary.ToolFailureRate(); !floatEq(got, 1.0/3.0) {
		t.Errorf("tool failure rate = %v, want %v", got, 1.0/3.0)
	}
}

func TestCostFromLocalTable(t *testing.T) {
	buf := runCascade(t, Options{Cost: flatCost})
	turns, _ := decodeLines(t, buf)
	// Turn 0: 1000 prompt @ $1/M + 200 completion @ $2/M = 0.001 + 0.0004.
	want := 1000*1.0/1_000_000 + 200*2.0/1_000_000
	if !floatEq(turns[0].Response.CostUSD, want) {
		t.Errorf("turn 0 cost = %v, want %v", turns[0].Response.CostUSD, want)
	}
}

func TestSecretsNeverAppear(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := New(Options{Writer: buf, SessionID: "s", Cost: flatCost, Verbose: true})
	ctx := context.Background()

	const apiKey = "sk-ant-api03-SUPERSECRETVALUE1234567890abcdef"
	const bearer = "Bearer eyJhbGciOiJIUzI1NiSECRETTOKEN"
	const awsKey = "AKIAIOSFODNN7EXAMPLE"

	// Secrets in the prompt (verbose), tool args, and tool result must all be scrubbed.
	rec.BeginTurn(ctx, BeginParams{
		Turn: 0, ServedModel: "m", BaseModel: "m",
		Prompt: "system prompt with ANTHROPIC_API_KEY=" + apiKey,
	})
	rec.SetResponse(ResponseParams{FinishReason: "tool_use", PromptTokens: 10, CompletionTokens: 5, Completion: "here is your key " + apiKey})
	rec.AddToolCall(ToolParams{
		Name:      "Bash",
		Arguments: `{"env":"` + awsKey + `","auth":"` + bearer + `"}`,
		Result:    `{"api_key":"` + apiKey + `"}`,
	})
	rec.Finish()

	out := buf.String()
	for _, secret := range []string{apiKey, "SUPERSECRETVALUE1234567890abcdef", "SECRETTOKEN", awsKey} {
		if strings.Contains(out, secret) {
			t.Errorf("secret leaked into action log: %q", secret)
		}
	}
	if !strings.Contains(out, redactPlaceholder) {
		t.Error("expected redaction placeholder in output")
	}
}

func TestPromptHashNotDumpedByDefault(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := New(Options{Writer: buf, SessionID: "s", Cost: flatCost}) // verbose off
	ctx := context.Background()
	rec.BeginTurn(ctx, BeginParams{Turn: 0, ServedModel: "m", BaseModel: "m", Prompt: "the full transcript that must not be dumped"})
	rec.SetResponse(ResponseParams{PromptTokens: 1, CompletionTokens: 1})
	rec.Finish()

	turns, _ := decodeLines(t, buf)
	if turns[0].Request.Prompt != "" {
		t.Error("full prompt captured without --trace-verbose")
	}
	if !strings.HasPrefix(turns[0].Request.PromptHash, "sha256:") {
		t.Errorf("prompt not referenced by hash: %q", turns[0].Request.PromptHash)
	}
	if strings.Contains(buf.String(), "the full transcript") {
		t.Error("prompt text leaked without verbose mode")
	}
}

func TestVerboseCapturesFullPrompt(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := New(Options{Writer: buf, SessionID: "s", Cost: flatCost, Verbose: true})
	ctx := context.Background()
	const prompt = "the full transcript captured under trace-verbose"
	rec.BeginTurn(ctx, BeginParams{Turn: 0, ServedModel: "m", BaseModel: "m", Prompt: prompt})
	rec.SetResponse(ResponseParams{PromptTokens: 1, CompletionTokens: 1})
	rec.Finish()

	turns, _ := decodeLines(t, buf)
	if turns[0].Request.Prompt != prompt {
		t.Errorf("verbose prompt = %q, want full text", turns[0].Request.Prompt)
	}
}

func TestNilRecorderIsNoOp(t *testing.T) {
	var rec *Recorder
	ctx := context.Background()
	// None of these must panic.
	rec.BeginTurn(ctx, BeginParams{Turn: 0})
	rec.SetResponse(ResponseParams{})
	rec.AddToolCall(ToolParams{Name: "x"})
	rec.FlushTurn()
	if rec.Finish() != nil {
		t.Error("nil recorder Finish should return nil")
	}
	if rec.Summary() != nil {
		t.Error("nil recorder Summary should return nil")
	}
}

func TestFinishIdempotent(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := New(Options{Writer: buf, SessionID: "s", Cost: flatCost})
	ctx := context.Background()
	rec.BeginTurn(ctx, BeginParams{Turn: 0, ServedModel: "m", BaseModel: "m"})
	rec.SetResponse(ResponseParams{PromptTokens: 10, CompletionTokens: 5})
	rec.Finish()
	before := buf.Len()
	rec.Finish() // second call must not double-write.
	if buf.Len() != before {
		t.Error("Finish is not idempotent")
	}
}

func TestNilWriterStillAggregates(t *testing.T) {
	// A recorder with no JSONL sink still tracks the summary (e.g. OTEL-only).
	rec := New(Options{Cost: flatCost, SessionID: "s"})
	ctx := context.Background()
	rec.BeginTurn(ctx, BeginParams{Turn: 0, ServedModel: "m", BaseModel: "m"})
	rec.SetResponse(ResponseParams{PromptTokens: 100, CompletionTokens: 50})
	s := rec.Finish()
	if s.Turns != 1 || s.PromptTokens != 100 {
		t.Errorf("summary not aggregated with nil writer: %+v", s)
	}
}

func floatEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-12
}
