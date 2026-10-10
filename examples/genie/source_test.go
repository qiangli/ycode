package genie

import (
	"testing"
)

func TestSourceEmbedsAllPromptProfiles(t *testing.T) {
	for _, name := range []string{
		"prompts/system.md",
		"prompts/system-general.md",
		"prompts/system-swe.md",
		"prompts/system-terminal.md",
		"prompts/system-yolo.md",
	} {
		data, err := Source.ReadFile(name)
		if err != nil {
			t.Fatalf("Source.ReadFile(%q): %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("Source file %q is empty", name)
		}
		if len(data) >= 4096 {
			t.Fatalf("Source file %q is %d bytes (exceeds 4096)", name, len(data))
		}
	}
}
