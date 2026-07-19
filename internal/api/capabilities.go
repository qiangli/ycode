package api

import "strings"

// ProviderCapabilities describes what a provider/model combination supports.
type ProviderCapabilities struct {
	// CachingSupported indicates explicit server-side prompt caching
	// (e.g., Anthropic prompt cache). When false, the prompt builder
	// may use differential context injection to reduce input tokens.
	CachingSupported bool `json:"cachingSupported"`

	// MaxContextTokens is the advertised context window size.
	// Zero means unknown (use conservative default).
	MaxContextTokens int `json:"maxContextTokens,omitempty"`
}

// DetectCapabilities returns known capabilities for a provider/model pair.
// This is a static lookup — no API call is made.
func DetectCapabilities(kind ProviderKind, model string) ProviderCapabilities {
	switch kind {
	case ProviderAnthropic:
		return anthropicCapabilities(model)
	case ProviderOpenAI:
		return openaiCapabilities(model)
	case ProviderGemini:
		return geminiCapabilities(model)
	default:
		// Unknown providers: assume no caching (safe default).
		return ProviderCapabilities{CachingSupported: false}
	}
}

// MergeCapabilities returns a copy of base with non-zero fields from overrides applied.
func MergeCapabilities(base, overrides ProviderCapabilities) ProviderCapabilities {
	result := base
	// A user-specified override always wins for caching.
	// We use a tri-state trick: overrides with MaxContextTokens > 0
	// signal intentional override. For CachingSupported, any explicit
	// config is treated as override (handled by the caller who sets it).
	if overrides.MaxContextTokens > 0 {
		result.MaxContextTokens = overrides.MaxContextTokens
	}
	return result
}

func anthropicCapabilities(model string) ProviderCapabilities {
	caps := ProviderCapabilities{CachingSupported: true}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "opus"):
		caps.MaxContextTokens = 200_000
	case strings.Contains(lower, "sonnet"):
		caps.MaxContextTokens = 200_000
	case strings.Contains(lower, "haiku"):
		caps.MaxContextTokens = 200_000
	default:
		caps.MaxContextTokens = 200_000
	}
	return caps
}

func geminiCapabilities(model string) ProviderCapabilities {
	caps := ProviderCapabilities{CachingSupported: true}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "pro"):
		caps.MaxContextTokens = 1_000_000
	case strings.Contains(lower, "flash"):
		caps.MaxContextTokens = 1_000_000
	default:
		caps.MaxContextTokens = 1_000_000
	}
	return caps
}

func openaiCapabilities(model string) ProviderCapabilities {
	// OpenAI proper has only weak implicit prefix caching, so differential context
	// injection (send just the changed sections after turn 1) wins there — keep it
	// off. But several OpenAI-COMPATIBLE providers ship STRONG, documented automatic
	// prefix caching: DeepSeek, Moonshot/Kimi, and z.ai GLM cache a byte-stable
	// prefix server-side at a large discount AND — the part that matters for
	// latency — do not reprocess it. For those, a stable prefix (cachingSupported=
	// true → the cache-friendly builder, not the cache-BREAKING differential one)
	// beats sending a diff: the cached prompt is ~4x cheaper and materially faster.
	// The cached subset is read back via prompt_tokens_details.cached_tokens
	// (Usage.foldOpenAICache) so pricing and the session summary see the hits.
	lower := strings.ToLower(model)
	autoCache := strings.Contains(lower, "kimi") || strings.Contains(lower, "moonshot") ||
		strings.Contains(lower, "deepseek") || strings.Contains(lower, "glm")
	caps := ProviderCapabilities{CachingSupported: autoCache}
	switch {
	case strings.Contains(lower, "gpt-4o"):
		caps.MaxContextTokens = 128_000
	case strings.Contains(lower, "gpt-4"):
		caps.MaxContextTokens = 128_000
	case strings.Contains(lower, "o3"), strings.Contains(lower, "o4"):
		caps.MaxContextTokens = 200_000
	case strings.Contains(lower, "glm"):
		// GLM-4.6: 200K context, 128K max output (docs.z.ai/guides/llm/glm-4.6).
		// Verified against the vendor docs, not recalled — the window drives every
		// context threshold now, so a wrong number here is a wrong number everywhere.
		caps.MaxContextTokens = 200_000
	case strings.Contains(lower, "deepseek"):
		caps.MaxContextTokens = 128_000
	default:
		caps.MaxContextTokens = 128_000
	}
	return caps
}
