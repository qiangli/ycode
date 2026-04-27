package prompt

import (
	"strings"
	"testing"
)

func TestRecommendedSkillInjection_WithCaching(t *testing.T) {
	mode := RecommendedSkillInjection(true)
	if mode != SkillInjectUser {
		t.Errorf("got %d, want SkillInjectUser (%d)", mode, SkillInjectUser)
	}
}

func TestRecommendedSkillInjection_WithoutCaching(t *testing.T) {
	mode := RecommendedSkillInjection(false)
	if mode != SkillInjectSystem {
		t.Errorf("got %d, want SkillInjectSystem (%d)", mode, SkillInjectSystem)
	}
}

func TestFormatSkillAsUserMessage(t *testing.T) {
	content := "Run make build and validate output."
	result := FormatSkillAsUserMessage(content)

	if !strings.HasPrefix(result, "[SYSTEM NOTE:") {
		t.Error("expected result to start with [SYSTEM NOTE:")
	}
	if !strings.Contains(result, content) {
		t.Error("expected result to contain skill content")
	}
	if !strings.Contains(result, "Do NOT acknowledge") {
		t.Error("expected result to contain instruction not to acknowledge")
	}
}
