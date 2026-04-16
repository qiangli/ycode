package api

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompressGzip_SmallInput(t *testing.T) {
	data := []byte("tiny")
	compressed, encoding := CompressGzip(data)
	if encoding != "" {
		t.Error("small input should not be compressed")
	}
	if !bytes.Equal(compressed, data) {
		t.Error("uncompressed output should match input")
	}
}

func TestCompressGzip_LargeInput(t *testing.T) {
	// JSON-like content compresses very well.
	data := []byte(strings.Repeat(`{"role":"user","content":"hello world"}`, 200))
	compressed, encoding := CompressGzip(data)

	if encoding != "gzip" {
		t.Fatalf("large input should be compressed, got encoding=%q", encoding)
	}
	if len(compressed) >= len(data) {
		t.Errorf("compressed (%d) should be smaller than original (%d)", len(compressed), len(data))
	}

	// Verify roundtrip.
	decompressed, err := DecompressGzip(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Error("decompressed data should match original")
	}
}

func TestCompressGzip_ThresholdBoundary(t *testing.T) {
	// Exactly at threshold — should attempt compression.
	data := []byte(strings.Repeat("x", MinCompressSize))
	_, encoding := CompressGzip(data)
	// Repetitive data compresses well, so it should be gzip.
	if encoding != "gzip" {
		t.Errorf("data at threshold should compress, got encoding=%q", encoding)
	}
}
