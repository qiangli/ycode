package tools

import (
	"testing"
)

func TestApplyBlocklistWithAllowlist(t *testing.T) {
	allowlist := []string{"Agent", "bash", "read_file", "MemorySave", "WebFetch"}
	blocklist := []string{"Agent", "MemorySave"}

	result := ApplyBlocklist(allowlist, blocklist)

	expected := map[string]bool{"bash": true, "read_file": true, "WebFetch": true}
	if len(result) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(result), result)
	}
	for _, name := range result {
		if !expected[name] {
			t.Errorf("unexpected tool %q in filtered list", name)
		}
	}
}

func TestApplyBlocklistWithNilAllowlist(t *testing.T) {
	blocklist := []string{"Agent", "Handoff"}

	result := ApplyBlocklist(nil, blocklist)

	if result != nil {
		t.Fatalf("expected nil result for nil allowlist, got %v", result)
	}
}

func TestApplyBlocklistWithEmptyBlocklist(t *testing.T) {
	allowlist := []string{"bash", "read_file", "Agent"}

	result := ApplyBlocklist(allowlist, nil)

	if len(result) != len(allowlist) {
		t.Fatalf("expected %d tools, got %d: %v", len(allowlist), len(result), result)
	}
	for i, name := range allowlist {
		if result[i] != name {
			t.Errorf("result[%d] = %q, want %q", i, result[i], name)
		}
	}
}

func TestApplyBlocklistWithEmptySliceBlocklist(t *testing.T) {
	allowlist := []string{"bash", "read_file"}

	result := ApplyBlocklist(allowlist, []string{})

	if len(result) != len(allowlist) {
		t.Fatalf("expected %d tools, got %d: %v", len(allowlist), len(result), result)
	}
}

func TestDefaultSubagentBlocklistNotEmpty(t *testing.T) {
	if len(DefaultSubagentBlocklist) == 0 {
		t.Fatal("DefaultSubagentBlocklist should not be empty")
	}
}
