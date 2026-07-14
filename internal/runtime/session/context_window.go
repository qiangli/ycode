package session

// Context management, in one place, with one mechanism.
//
// There used to be five, arranged as a "4-layer context defense":
//
//	Layer 0  mask       replace old tool results with <MASKED>
//	Layer 1a soft trim  head/tail old tool results
//	Layer 1b hard clear replace old tool results with a placeholder
//	Layer 2  compact    summarize the conversation
//	  plus   route      classify a tool result and DELETE ITS MIDDLE
//	  plus   distill    head/tail a tool result at 1000 chars
//
// All of them were broken, and broken the same way: each decided to damage the model's
// observations by comparing an ESTIMATE of the conversation size (4 chars ≈ 1 token)
// against an ABSOLUTE CONSTANT that had nothing to do with the model on the other end.
//
// On a small model the constants sat OUTSIDE the window, so the machinery was inert and
// logged "context: healthy" right up to the API rejecting the request. On a large one
// they fired far too early — on turn 1 — and cut the middle out of the very file the
// model had just been told to implement against. It then spent seventeen turns on cat,
// sed, awk, base64 and xxd trying to rebuild what we had deleted.
//
// The layers existed to HEDGE. They hedged because the number underneath them was a
// guess, and nobody trusts a guess enough to act on it once — so instead they acted on
// it five times, gently. Five gentle wrong answers.
//
// The provider tells us the real number. It is in every response, and ycode was already
// declaring a field for it (ConversationMessage.Usage) that nothing ever wrote.
//
//	Ask the artifact.
//
// So: ONE question — is this conversation actually too big for THIS model? — asked
// against the count the provider reported, and ONE answer: compact it. Below that, the
// model gets exactly what the tools returned, byte for byte.

// AbsoluteToolOutputCap is a SAFETY limit, not context management.
//
// A pathological tool result — a core dump, a 200MB log, a binary opened by mistake —
// must not be inlined into a request whatever the pressure. That is a different concern
// from "the window is filling up", and it is the only unconditional limit that survives.
//
// It sits far above anything a real file read produces, so in normal operation it never
// fires and the model sees precisely what the tool returned.
const AbsoluteToolOutputCap = 256 * 1024

// CapToolOutput enforces AbsoluteToolOutputCap, and says so when it bites.
//
// Unlike everything it replaces, it names the size, explains why, and tells the model
// how to get the rest. A cut the model knows about costs it one follow-up call. A cut it
// does not know about costs it seventeen.
func CapToolOutput(output string) string {
	if len(output) <= AbsoluteToolOutputCap {
		return output
	}
	const half = AbsoluteToolOutputCap / 2
	return output[:half] +
		"\n\n[... " + itoa(len(output)-AbsoluteToolOutputCap) + " bytes omitted: this tool result exceeds the " +
		itoa(AbsoluteToolOutputCap/1024) + "KB safety cap for a single result. " +
		"Re-run with a narrower query (grep, head -n, a line range) to read the middle. ...]\n\n" +
		output[len(output)-half:]
}

// TokensUsed is how full the window is.
//
// Reported is what the PROVIDER said the last request cost. Unreported is our estimate
// of everything appended since — the tool results from the turn that just ran, which by
// definition no response has counted yet.
//
// The split is the point. The estimator (4 chars ≈ 1 token) is too crude to be trusted
// with a whole conversation; that is what produced five hedging layers. It is entirely
// adequate for the tail of one turn, sitting on top of a number that is exact.
type TokensUsed struct {
	Reported   int  // the provider's count for the last request
	Unreported int  // our estimate of what has been appended since
	HasReport  bool // false only on the very first request of a brand-new conversation
}

// Total is the best estimate of the window occupancy of the request about to be sent.
func (t TokensUsed) Total() int { return t.Reported + t.Unreported }

// MeasureTokens reports how full the window is, using the provider's own numbers
// wherever they exist and estimating only the tail they cannot cover.
//
// It walks back to the last assistant message the provider gave a token count for. That
// count covers the ENTIRE request that produced it — system prompt, tool schemas, every
// prior message — so everything at or before that message is exactly accounted for, and
// only what follows needs estimating.
//
// This is also what makes a RESUMED session safe. Usage is persisted per message, so a
// conversation loaded from disk with 90K tokens of history arrives with its 90K already
// known. Without it, the first request after a resume would have no count, "nothing to
// compact" would be the answer, and the request would blow the window — a success state
// reached by the ABSENCE of a measurement.
func MeasureTokens(messages []ConversationMessage) TokensUsed {
	for i := len(messages) - 1; i >= 0; i-- {
		u := messages[i].Usage
		if u == nil {
			continue
		}
		reported := u.InputTokens + u.OutputTokens + u.CacheReadInput + u.CacheCreationInput
		if reported <= 0 {
			continue
		}
		// Anthropic reports input_tokens and cache_read_input_tokens SEPARATELY, so
		// summing is correct. The OpenAI-compatible path never populates the cache
		// fields at all, so it cannot double-count either.
		unreported := 0
		for _, m := range messages[i+1:] {
			unreported += EstimateMessageTokens(m)
		}
		return TokensUsed{Reported: reported, Unreported: unreported, HasReport: true}
	}

	// No response has ever come back for this conversation. Estimate the lot — it is
	// all we have, and a conversation with no assistant turn in it is small.
	total := 0
	for _, m := range messages {
		total += EstimateMessageTokens(m)
	}
	return TokensUsed{Unreported: total}
}

// NeedsCompaction — the window is genuinely too full. Summarize, and pay for it.
func (b ContextBudget) NeedsCompaction(used TokensUsed) bool {
	if b.ContextWindow <= 0 {
		return false
	}
	return b.ShouldCompact(used.Total())
}

// NeedsTrim — the window is filling up, but compaction is not yet warranted.
//
// This is the cheap rung below compaction: drop stale tool observations, which costs
// nothing, instead of summarizing the conversation, which costs an LLM call. Without a
// rung here, EVERY pressure event on a long non-caching session pays for a full
// summarization round-trip.
//
// It is deliberately a rung and not a fifth mechanism: same measurement, same budget,
// one threshold lower.
func (b ContextBudget) NeedsTrim(used TokensUsed) bool {
	if b.ContextWindow <= 0 {
		return false
	}
	return used.Total() >= b.SoftTrimAt()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
