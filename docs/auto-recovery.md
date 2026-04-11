# Auto-Recovery from Token Limit Errors

ycode now automatically recovers from model token limit errors by using memory compaction techniques inspired by Claw Code.

## Overview

When a conversation exceeds the model's context window (token limit), ycode will:

1. **Detect** the token limit error from API responses
2. **Compact** older conversation history into a summary
3. **Retry** the request with the compacted context
4. **Notify** the user about the compaction

## How It Works

### Error Detection

The system recognizes various token limit error formats from different providers:

- OpenAI style: `"exceeded model token limit: 262144 (requested: 876782)"`
- Anthropic style: `"maximum context length is 200000 tokens..."`
- Generic: `"context length exceeded"`, `"too large"`, `"too long"`

### Compaction Strategy

When a token limit is hit:

1. **Preserve Recent Messages**: Keeps the last 4 messages verbatim to maintain context
2. **Summarize Older Messages**: Generates a structured summary of earlier conversation
3. **Never Split Tool Pairs**: Ensures tool_use/tool_result pairs stay together

The summary includes:
- Message counts by role (user/assistant/tool)
- Tools mentioned in the conversation
- Recent user requests (last 3)
- Pending work (inferred from keywords like "todo", "next", "pending")
- Key files referenced
- Current work excerpt

### User Experience

When compaction occurs, users see:

```
⚠ Context compacted: 12 messages summarized, 4 recent messages preserved.
```

The conversation continues seamlessly from where it left off.

## Technical Implementation

### Key Components

1. **`internal/api/errors.go`**: Error detection and parsing
   - `TokenLimitError` type
   - `ParseTokenLimitError()` function
   - `IsTokenLimitError()` helper

2. **`internal/runtime/conversation/runtime.go`**: Recovery logic
   - `TurnWithRecovery()` method
   - `RecoveryResult` struct
   - Message compaction and retry logic

3. **`internal/cli/app.go` & `tui.go`**: Integration
   - One-shot mode uses `TurnWithRecovery()`
   - TUI displays recovery information

### Configuration

No configuration required - auto-recovery is always enabled.

## Comparison with Claw Code

| Aspect | Claw Code | ycode |
|--------|-----------|-------|
| Trigger | 100K token threshold (proactive) | On error (reactive) |
| Preserve count | 4 messages | 4 messages |
| Summary format | Structured with priority tiers | Structured with XML tags |
| Multiple rounds | Yes | No (single compaction) |
| User notification | Silent | Explicit message |

## Future Improvements

Potential enhancements:

1. **Proactive Compaction**: Compact before hitting the limit based on estimated tokens
2. **Multiple Compaction Rounds**: Handle edge cases where one compaction isn't enough
3. **Smart Context Selection**: Use relevance scoring to preserve most important messages
4. **Hierarchical Summaries**: Nested summaries for very long conversations
