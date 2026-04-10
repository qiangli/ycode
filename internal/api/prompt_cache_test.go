package api

import (
	"testing"
)

func TestFingerprint(t *testing.T) {
	req1 := &Request{Model: "model1", System: "sys1"}
	req2 := &Request{Model: "model1", System: "sys1"}
	req3 := &Request{Model: "model2", System: "sys1"}

	fp1 := Fingerprint(req1)
	fp2 := Fingerprint(req2)
	fp3 := Fingerprint(req3)

	if fp1.ModelHash != fp2.ModelHash {
		t.Error("same model should produce same hash")
	}
	if fp1.ModelHash == fp3.ModelHash {
		t.Error("different model should produce different hash")
	}
	if fp1.SystemHash != fp2.SystemHash {
		t.Error("same system should produce same hash")
	}
}

func TestPromptCache_Check(t *testing.T) {
	cache := NewPromptCache()

	fp := &PromptFingerprint{
		ModelHash:  "aaa",
		SystemHash: "bbb",
		ToolsHash:  "ccc",
	}

	// First check should miss.
	if cache.Check(fp) {
		t.Error("first check should miss")
	}

	// Update cache.
	cache.Update(fp)

	// Same fingerprint should hit.
	if !cache.Check(fp) {
		t.Error("same fingerprint should hit")
	}

	// Different fingerprint should miss.
	fp2 := &PromptFingerprint{
		ModelHash:  "xxx",
		SystemHash: "bbb",
		ToolsHash:  "ccc",
	}
	if cache.Check(fp2) {
		t.Error("different fingerprint should miss")
	}
}

func TestPromptCache_DetectBreak(t *testing.T) {
	cache := NewPromptCache()

	// First call establishes baseline.
	if cache.DetectBreak(50000) {
		t.Error("first call should not detect break")
	}

	// Small change - no break.
	if cache.DetectBreak(49000) {
		t.Error("small change should not be a break")
	}

	// Large drop - break.
	if !cache.DetectBreak(45000) {
		t.Error("large drop should be a break")
	}
}

func TestPromptCache_Stats(t *testing.T) {
	cache := NewPromptCache()

	fp := &PromptFingerprint{ModelHash: "a", SystemHash: "b", ToolsHash: "c"}

	cache.Check(fp) // miss
	cache.Update(fp)
	cache.Check(fp) // hit
	cache.Check(fp) // hit

	if cache.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", cache.Hits)
	}
	if cache.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", cache.Misses)
	}
	if cache.Writes != 1 {
		t.Errorf("expected 1 write, got %d", cache.Writes)
	}
}
