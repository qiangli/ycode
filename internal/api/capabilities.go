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

func openaiCapabilities(model string) ProviderCapabilities {
	// OpenAI has implicit prefix caching for identical prefixes >1024 tokens,
	// but it's automatic and not as controllable as Anthropic's explicit cache.
	// Treat as unsupported to enable differential context injection.
	caps := ProviderCapabilities{CachingSupported: false}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "gpt-4o"):
		caps.MaxContextTokens = 128_000
	case strings.Contains(lower, "gpt-4"):
		caps.MaxContextTokens = 128_000
	case strings.Contains(lower, "o3"), strings.Contains(lower, "o4"):
		caps.MaxContextTokens = 200_000
	default:
		caps.MaxContextTokens = 128_000
	}
	return caps
}
