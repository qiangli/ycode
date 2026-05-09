package selfinit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarker_RoundTrip(t *testing.T) {
	repo := t.TempDir()
	if MarkerMatches(repo, "x") {
		t.Errorf("missing marker should not match")
	}
	if err := WriteMarker(repo, "abc123"); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	if !MarkerMatches(repo, "abc123") {
		t.Errorf("MarkerMatches false negative")
	}
	if MarkerMatches(repo, "different") {
		t.Errorf("MarkerMatches false positive")
	}
}

func TestStateHash_OrderIndependent(t *testing.T) {
	caps := []CapabilitySpec{
		{Name: "ycode-loom", Family: "loom"},
		{Name: "ycode-pulse", Family: "pulse"},
	}
	rev := []CapabilitySpec{caps[1], caps[0]}
	if a, b := stateHash("v1", caps, []string{"claude"}), stateHash("v1", rev, []string{"claude"}); a != b {
		t.Errorf("hash should be order-independent\na=%s\nb=%s", a, b)
	}
	// Different version flips it.
	if a, b := stateHash("v1", caps, nil), stateHash("v2", caps, nil); a == b {
		t.Errorf("hash should change on version bump")
	}
	// Different tools flip it.
	if a, b := stateHash("v1", caps, []string{"claude"}), stateHash("v1", caps, []string{"opencode"}); a == b {
		t.Errorf("hash should change on tool list")
	}
}

func TestOptOut(t *testing.T) {
	repo := t.TempDir()
	if IsOptedOut(repo) {
		t.Errorf("fresh repo should not be opted out")
	}
	if err := WriteOptOut(repo); err != nil {
		t.Fatalf("WriteOptOut: %v", err)
	}
	if !IsOptedOut(repo) {
		t.Errorf("WriteOptOut did not register")
	}
	// File exists where expected.
	if _, err := os.Stat(filepath.Join(repo, ".agents", "ycode", noInitFilename)); err != nil {
		t.Errorf("opt-out file missing: %v", err)
	}
}
