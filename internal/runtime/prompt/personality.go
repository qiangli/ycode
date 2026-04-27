package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Personality represents a named personality preset.
type Personality struct {
	Name        string
	Description string
	Content     string // the actual prompt text injected into system prompt
}

// BuiltinPersonalities are the available presets.
var BuiltinPersonalities = map[string]*Personality{
	"default": {
		Name:        "default",
		Description: "Helpful, knowledgeable, and direct",
		Content:     "", // no extra personality injection for default
	},
	"pirate": {
		Name:        "pirate",
		Description: "Speaks like a pirate",
		Content:     "Respond in the style of a pirate. Use pirate slang, say 'arr' and 'matey', refer to code as 'treasure' and bugs as 'sea monsters'. But still be technically accurate and helpful.",
	},
	"shakespeare": {
		Name:        "shakespeare",
		Description: "Speaks in Shakespearean English",
		Content:     "Respond in the style of Shakespeare. Use 'thee', 'thou', 'hath', 'doth', 'forsooth', and other Elizabethan English. Structure responses with dramatic flair, but remain technically precise.",
	},
	"stern": {
		Name:        "stern",
		Description: "Extremely concise and no-nonsense",
		Content:     "Be extremely terse and direct. No pleasantries, no filler words, no encouragement. Just state facts and give instructions. If something is wrong, say so bluntly.",
	},
	"teacher": {
		Name:        "teacher",
		Description: "Patient and educational, explains concepts",
		Content:     "Adopt a patient, educational tone. Explain the 'why' behind every suggestion. Use analogies to make complex concepts accessible. Ask if the user wants more detail on any point.",
	},
	"kawaii": {
		Name:        "kawaii",
		Description: "Cute and enthusiastic with emoticons",
		Content:     "Be enthusiastic and supportive! Use emoticons like (◕‿◕), (•◡•), ✨, and 💫. Celebrate successes with the user. Stay technically accurate but make coding feel fun and approachable.",
	},
}

// LoadSOUL reads a SOUL.md file for user-defined identity.
// Returns empty string if the file doesn't exist.
func LoadSOUL(projectRoot string) string {
	// Check project root first, then home directory.
	candidates := []string{
		filepath.Join(projectRoot, "SOUL.md"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".ycode", "SOUL.md"))
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// PersonalitySection builds the personality prompt section.
// Priority: SOUL.md > named personality > nothing.
func PersonalitySection(soulContent string, personalityName string) string {
	if soulContent != "" {
		return fmt.Sprintf("# Identity\n%s", soulContent)
	}
	if personalityName != "" && personalityName != "default" {
		if p, ok := BuiltinPersonalities[personalityName]; ok && p.Content != "" {
			return fmt.Sprintf("# Personality\n%s", p.Content)
		}
	}
	return ""
}

// ListPersonalities returns all available personality names.
func ListPersonalities() []string {
	var names []string
	for name := range BuiltinPersonalities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
