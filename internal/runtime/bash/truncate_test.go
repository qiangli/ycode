package bash

import (
	"strings"
	"testing"
)

func TestTruncateOutput_Small(t *testing.T) {
	input := "hello world\n"
	got := TruncateOutput(input, MaxOutputSize)
	if got != "hello world" {
		t.Errorf("expected trailing newline trimmed, got %q", got)
	}
}

func TestTruncateOutput_ExactSize(t *testing.T) {
	input := strings.Repeat("x", MaxOutputSize)
	got := TruncateOutput(input, MaxOutputSize)
	if len(got) != MaxOutputSize {
		t.Errorf("expected len %d, got %d", MaxOutputSize, len(got))
	}
}

func TestTruncateOutput_Large_PreservesHeadAndTail(t *testing.T) {
	// Build input: HEAD...MIDDLE...TAIL
	headContent := "=== HEAD START ===\n"
	tailContent := "\n=== TAIL END ==="
	middleSize := MaxOutputSize * 2
	middle := strings.Repeat("M", middleSize)
	input := headContent + middle + tailContent

	got := TruncateOutput(input, MaxOutputSize)

	if len(got) > MaxOutputSize {
		t.Errorf("output exceeds maxSize: got %d, want <= %d", len(got), MaxOutputSize)
	}
	if !strings.HasPrefix(got, "=== HEAD START ===") {
		t.Error("head content not preserved")
	}
	if !strings.HasSuffix(got, "=== TAIL END ===") {
		t.Error("tail content not preserved")
	}
	if !strings.Contains(got, "bytes omitted") {
		t.Error("omission marker missing")
	}
}

func TestTruncateOutput_TailBias(t *testing.T) {
	// Verify ~70% of space goes to tail.
	input := strings.Repeat("H", 100000) + strings.Repeat("T", 100000)
	maxSize := 1000
	got := TruncateOutput(input, maxSize)

	markerIdx := strings.Index(got, "[...")
	if markerIdx < 0 {
		t.Fatal("marker not found")
	}
	headLen := markerIdx
	// Tail starts after marker end
	markerEnd := strings.Index(got, "...]\n\n")
	if markerEnd < 0 {
		t.Fatal("marker end not found")
	}
	tailLen := len(got) - (markerEnd + len("...]\n\n"))

	// Head should be ~30% of available, tail ~70%.
	ratio := float64(tailLen) / float64(headLen+tailLen)
	if ratio < 0.60 || ratio > 0.80 {
		t.Errorf("tail ratio %.2f outside expected range [0.60, 0.80]; head=%d tail=%d", ratio, headLen, tailLen)
	}
}

func TestTruncateOutput_EmptyString(t *testing.T) {
	got := TruncateOutput("", MaxOutputSize)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
