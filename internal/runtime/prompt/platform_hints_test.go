package prompt

import (
	"strings"
	"testing"
)

func TestPlatformHintsSection_KnownChannel(t *testing.T) {
	result := PlatformHintsSection("telegram")

	if !strings.HasPrefix(result, "# Platform") {
		t.Error("should start with Platform heading")
	}
	if !strings.Contains(result, "Telegram") {
		t.Error("should contain Telegram-specific hints")
	}
}

func TestPlatformHintsSection_CLI(t *testing.T) {
	result := PlatformHintsSection("cli")

	if result != "" {
		t.Errorf("PlatformHintsSection for cli = %q, want empty string", result)
	}
}

func TestPlatformHintsSection_Unknown(t *testing.T) {
	result := PlatformHintsSection("carrier-pigeon")

	if result != "" {
		t.Errorf("PlatformHintsSection for unknown = %q, want empty string", result)
	}
}

func TestPlatformHintsSection_AllChannels(t *testing.T) {
	for channel, hint := range PlatformHints {
		result := PlatformHintsSection(channel)
		if hint == "" {
			if result != "" {
				t.Errorf("channel %q has empty hint but PlatformHintsSection returned %q", channel, result)
			}
		} else {
			if !strings.Contains(result, hint) {
				t.Errorf("channel %q: result should contain the hint text", channel)
			}
		}
	}
}
