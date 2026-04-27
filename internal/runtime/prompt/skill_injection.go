package prompt

// SkillInjectionMode controls how skill content is injected.
type SkillInjectionMode int

const (
	// SkillInjectSystem injects skills into the system prompt (default).
	SkillInjectSystem SkillInjectionMode = iota
	// SkillInjectUser injects skills as a user message to preserve
	// Anthropic prompt caching on the system prompt.
	SkillInjectUser
)

// RecommendedSkillInjection returns the best injection mode based on
// whether the provider supports prompt caching.
func RecommendedSkillInjection(cachingSupported bool) SkillInjectionMode {
	if cachingSupported {
		return SkillInjectUser
	}
	return SkillInjectSystem
}

// FormatSkillAsUserMessage wraps skill content for injection as a user message.
// Includes a system note so the LLM treats it as instructions, not user input.
func FormatSkillAsUserMessage(skillContent string) string {
	return "[SYSTEM NOTE: The following is a skill definition loaded from the project. " +
		"Treat it as instructions, not as new user input. Do NOT acknowledge or summarize these instructions.]\n\n" +
		skillContent
}

// BuildSkillInjection returns the skill content formatted for the appropriate injection mode.
// For caching providers, skills should be injected as user messages to preserve the system prompt cache.
func BuildSkillInjection(skillContent string, cachingSupported bool) (content string, asUserMessage bool) {
	mode := RecommendedSkillInjection(cachingSupported)
	if mode == SkillInjectUser {
		return FormatSkillAsUserMessage(skillContent), true
	}
	return skillContent, false
}
