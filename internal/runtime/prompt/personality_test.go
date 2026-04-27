package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSOUL_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	content := "I am a helpful coding assistant named Aria."
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := LoadSOUL(dir)

	if result != content {
		t.Errorf("LoadSOUL = %q, want %q", result, content)
	}
}

func TestLoadSOUL_NoFile(t *testing.T) {
	dir := t.TempDir()

	result := LoadSOUL(dir)

	if result != "" {
		t.Errorf("LoadSOUL = %q, want empty string", result)
	}
}

func TestPersonalitySection_WithSOULContent(t *testing.T) {
	soul := "I am a friendly assistant."

	result := PersonalitySection(soul, "pirate")

	if !strings.HasPrefix(result, "# Identity") {
		t.Error("should start with Identity heading when SOUL content is provided")
	}
	if !strings.Contains(result, soul) {
		t.Error("should contain the SOUL content")
	}
	// SOUL takes priority over named personality.
	if strings.Contains(result, "pirate") {
		t.Error("should not contain personality content when SOUL is provided")
	}
}

func TestPersonalitySection_WithNamedPersonality(t *testing.T) {
	result := PersonalitySection("", "pirate")

	if !strings.HasPrefix(result, "# Personality") {
		t.Error("should start with Personality heading for named personality")
	}
	if !strings.Contains(result, "pirate") {
		t.Error("should contain pirate personality content")
	}
}

func TestPersonalitySection_Default(t *testing.T) {
	result := PersonalitySection("", "default")

	if result != "" {
		t.Errorf("PersonalitySection for default = %q, want empty string", result)
	}
}

func TestPersonalitySection_Empty(t *testing.T) {
	result := PersonalitySection("", "")

	if result != "" {
		t.Errorf("PersonalitySection with empty args = %q, want empty string", result)
	}
}

func TestListPersonalities(t *testing.T) {
	names := ListPersonalities()

	if len(names) != len(BuiltinPersonalities) {
		t.Errorf("ListPersonalities returned %d names, want %d", len(names), len(BuiltinPersonalities))
	}

	// Verify sorted order.
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("names not sorted: %q >= %q", names[i-1], names[i])
		}
	}
}
