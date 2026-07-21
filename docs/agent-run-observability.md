# Agent-run observability — the per-turn action log

**Status:** implemented (ycode P1 of the umbrella design of record,
`docs/agent-run-observability-otel.md`). Client-side, local-first, no gateway or
billing API in the loop.

## Why

Before this, answering *"how many turns did a run take, which model actually ran
each turn, did the premium model ever intervene, and what did it cost"* meant
grepping PTY logs — which don't record per-turn model or billed tokens. A cascade
run that looped 117 turns could not tell you whether the premium model ever ran.
The action log makes served-model, escalation, tool-failure loops, and cost
**directly observable**, per turn, as structured data.

The principle is that the **client is the source of truth**: the code that builds
the request knows the served model and params before the wire, and the code that
reads the response gets `usage {prompt_tokens, completion_tokens}` and the finish
reason directly. Cost is those tokens × a local price table
(`internal/runtime/usage`). No provider dashboard, no admin key, no gateway.

## What it produces

Every ycode session writes a JSONL action log, one object per line:

```
<otel-data-dir>/instances/<instance-id>/actions.jsonl
```

`<otel-data-dir>` defaults to `~/.agents/ycode/otel` (overridable via
`observability.dataDir` in settings.json or `OTEL_STORAGE_PATH`). Each turn is
one `"type":"turn"` line; the session ends with one `"type":"summary"` line.

When an OTEL endpoint is configured, the same data is also emitted as an
`agent.turn` span (attributes: `served_model`, `base_model`, `escalated`,
`reason`, `provider`, `from_provider`, `prompt_tokens`, `completion_tokens`,
`cost_usd`, `finish_reason`) with a `tool.call` span event per tool call. The
JSONL is self-sufficient and works air-gapped; the span is an additional
vantage point, never a dependency.

## Fleet metrics

Each flushed turn is also published as OTEL metrics, from the same code path
that writes the JSONL line — the two can never disagree:

| Metric | Type | Attributes | Meaning |
| --- | --- | --- | --- |
| `fleet.tokens` | counter | `model`, `provider`, `kind` (`prompt` / `completion` / `cache_read` / `cache_write`) | billed tokens per turn |
| `fleet.cost_usd` | counter (USD) | `model`, `provider` | locally-priced spend |
| `fleet.escalation` | counter | `base_model`, `served_model`, `provider`, `reason` | base→premium **switches** — a run that escalates once and stays there counts 1, not once per premium turn |

The names are fleet-scoped rather than ycode-scoped on purpose: the point of a
per-turn record is comparing agents and models across tools, and a metric named
after its emitter cannot be summed with its peers.

Metrics ride the standard OTEL env vars and are a **total no-op** when no
meter provider is configured (`OTEL_EXPORTER_OTLP_ENDPOINT` unset and no file
exporter): the instruments bind to the SDK's no-op global and export nothing.
Gate tests: `internal/observe/fleet_otel_test.go` (in-process span recorder +
manual metric reader).

## Turn record

```jsonc
{
  "type": "turn",
  "session_id": "…",
  "turn": 7,
  "started_at": "2026-07-20T17:14:24Z",
  "ended_at":   "2026-07-20T17:14:39Z",
  "duration_ms": 15021,
  "request": {
    "served_model": "claude-opus-4-8",   // the model the cascade ACTUALLY used this turn
    "base_model":   "glm-4.6",            // the configured base
    "escalated":    true,                 // served_model != base_model
    "reason":       "model_override",     // why, when known
    "provider":     "anthropic",
    "temperature":  null,
    "max_tokens":   32000,
    "reasoning_effort": "",
    "prompt_tokens": 24310,               // billed prompt size (from response usage)
    "prompt_hash":  "sha256:1a2b3c4d…"    // a reference, NOT the prompt text
    // "prompt": "…"                      // full text ONLY under --trace-verbose
  },
  "response": {
    "finish_reason":     "tool_use",
    "completion":        "…",             // redacted, truncated unless verbose
    "completion_hash":   "sha256:…",
    "prompt_tokens":     24310,
    "completion_tokens": 512,
    "cost_usd":          0.0731           // tokens × local price table
  },
  "tool_calls": [
    {
      "name": "Bash",
      "arguments": "{\"command\":\"go build ./...\"}",  // redacted
      "result": "…",                                    // redacted, truncated
      "status": "ok",                                   // "ok" | "error"
      "duration_ms": 4210
    }
  ]
}
```

`escalated=true` is the direct, per-turn premium-intervention signal that
replaces log-grepping. Cascade escalation (`docs/cascade-escalation.md`) now
switches the served model mid-run, and it lands here automatically — the
recorder captures whatever model the client actually put on the wire, with the
escalator's reason (`loop`, `stall_x3`, …) in `request.reason`.

## Session summary

```jsonc
{
  "type": "summary",
  "session_id": "…",
  "turns": 42,
  "escalations": 3,
  "prompt_tokens": 812340,
  "completion_tokens": 20113,
  "tool_calls": 88,
  "tool_failures": 4,
  "cost_usd": 2.41,
  "per_model": { "glm-4.6": {"turns": 39, …}, "claude-opus-4-8": {"turns": 3, "escalations": 3, …} },
  "per_tool":  { "Bash": {"calls": 40, "failures": 3}, "Read": {"calls": 30, "failures": 0} },
  "started_at": "…", "ended_at": "…"
}
```

Summary totals are aggregated from the exact per-turn records as they are
flushed, so **the summary always reconciles with the sum of the turn lines** by
construction.

## Redaction — secrets never appear

- **Prompts are referenced by hash, not dumped.** The full prompt is captured
  only under `--trace-verbose` (or `YCODE_TRACE_VERBOSE=1`), and is redacted even
  then.
- **Vault secrets and API keys are scrubbed** from every free-text field (tool
  arguments, tool results, completion, verbose prompt) before it is written:
  keyed values (`api_key`, `token`, `secret`, `authorization`, …), `Bearer`
  tokens, and vendor-prefixed keys (`sk-ant-…`, `sk-…`, `gh[pousr]_…`, `xox…`,
  `AKIA…`, `AIza…`) become `[REDACTED]`, keeping the key name so the log still
  shows *that* a credential was present.

## Configuration

| Control | Effect |
| --- | --- |
| `--trace-verbose` (flag) / `YCODE_TRACE_VERBOSE=1` | capture full prompts + untruncated text (still redacted) |
| `YCODE_ACTION_LOG=off` | disable the action log |
| `observability.dataDir` / `OTEL_STORAGE_PATH` | change where `actions.jsonl` is written |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | additionally emit `agent.turn` spans to a collector |

## Implementation

- `internal/observe` — the recorder: data model (`TurnRecord`, `Summary`),
  JSONL + span emission, redaction, and cost via `internal/runtime/usage`. Pure,
  fully unit-tested (`internal/observe/recorder_test.go`) with no ycode-session
  dependency.
- `internal/runtime/conversation/runtime.go` — the wiring: `Turn()` opens the
  turn record (`BeginTurn`) and fills the response (`SetResponse`);
  `executeToolsSequential`/`executeToolsParallel` time each call and record it
  (`AddToolCall`); `ExecuteTools` flushes the completed turn.
  `action_log_test.go` drives the full loop through a scripted provider — no live
  LLM required.
- `cmd/ycode/action_log.go` + `cmd/ycode/otel.go` — construct the recorder,
  point it at `actions.jsonl`, and write the summary on shutdown.
