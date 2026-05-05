package features

import (
	"strings"
	"testing"
)

func TestEmbeddedRegistryLoads(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Features) == 0 {
		t.Fatal("registry is empty")
	}
	// Sanity: at least one stable and one experimental at the audit baseline.
	if len(reg.ByTier(TierStable)) == 0 {
		t.Error("expected at least one stable feature in the embedded registry")
	}
	if len(reg.ByTier(TierExperimental)) == 0 {
		t.Error("expected at least one experimental feature in the embedded registry")
	}
}

func TestParseInvalidTier(t *testing.T) {
	data := []byte("features:\n  - name: bad\n    tier: nonexistent\n")
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for invalid tier")
	}
	if !strings.Contains(err.Error(), "invalid tier") {
		t.Fatalf("error %q does not mention invalid tier", err)
	}
}

func TestParseDuplicateNames(t *testing.T) {
	data := []byte("features:\n  - name: dup\n    tier: stable\n  - name: dup\n    tier: experimental\n")
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error %q does not mention duplicate", err)
	}
}

func TestParseMissingName(t *testing.T) {
	data := []byte("features:\n  - tier: stable\n")
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected missing-name error")
	}
}

func TestByTier(t *testing.T) {
	data := []byte("features:\n  - name: a\n    tier: stable\n  - name: b\n    tier: experimental\n  - name: c\n    tier: stable\n")
	reg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(reg.ByTier(TierStable)); got != 2 {
		t.Errorf("expected 2 stable features, got %d", got)
	}
	if got := len(reg.ByTier(TierExperimental)); got != 1 {
		t.Errorf("expected 1 experimental feature, got %d", got)
	}
	if got := len(reg.ByTier(TierWIP)); got != 0 {
		t.Errorf("expected 0 wip features, got %d", got)
	}
}

func TestGet(t *testing.T) {
	data := []byte("features:\n  - name: foo\n    tier: stable\n")
	reg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := reg.Get("foo"); !ok {
		t.Error("expected to find foo")
	}
	if _, ok := reg.Get("missing"); ok {
		t.Error("expected missing to be absent")
	}
}

func TestTierValid(t *testing.T) {
	if !TierStable.Valid() || !TierExperimental.Valid() || !TierWIP.Valid() {
		t.Error("known tiers should be valid")
	}
	if Tier("nope").Valid() {
		t.Error("unknown tier should be invalid")
	}
}
