package bash

import (
	"strings"
	"testing"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("hello"))
	if got := rb.String(); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("0123456789abcdef"))
	got := rb.String()
	if got != "6789abcdef" {
		t.Errorf("expected last 10 bytes '6789abcdef', got %q", got)
	}
	if rb.TotalWritten() != 16 {
		t.Errorf("expected TotalWritten=16, got %d", rb.TotalWritten())
	}
}

func TestRingBuffer_MultipleWrites(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("12345"))
	rb.Write([]byte("67890"))
	if got := rb.String(); got != "1234567890" {
		t.Errorf("expected '1234567890', got %q", got)
	}
	rb.Write([]byte("XY"))
	if got := rb.String(); got != "34567890XY" {
		t.Errorf("expected '34567890XY', got %q", got)
	}
}

func TestRingBuffer_Incremental_FirstCall(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("first"))
	got := rb.Incremental()
	if got != "first" {
		t.Errorf("first incremental should return all, got %q", got)
	}
}

func TestRingBuffer_Incremental_Subsequent(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("first"))
	rb.Incremental() // consume
	rb.Write([]byte("second"))
	got := rb.Incremental()
	if got != "second" {
		t.Errorf("expected 'second', got %q", got)
	}
}

func TestRingBuffer_Incremental_NoNewData(t *testing.T) {
	rb := NewRingBuffer(100)
	rb.Write([]byte("data"))
	rb.Incremental()
	got := rb.Incremental()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRingBuffer_Incremental_Overflow(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("12345"))
	rb.Incremental() // consume
	// Write more than buffer size since last read.
	rb.Write([]byte("abcdefghijklmnop")) // 16 bytes, buffer holds last 10
	got := rb.Incremental()
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice, got %q", got)
	}
	if !strings.Contains(got, "ghijklmnop") {
		t.Errorf("expected last 10 bytes in output, got %q", got)
	}
}

func TestRingBuffer_LargeWrite(t *testing.T) {
	rb := NewRingBuffer(10)
	data := strings.Repeat("x", 1000)
	rb.Write([]byte(data))
	if rb.Len() != 10 {
		t.Errorf("expected len=10, got %d", rb.Len())
	}
	if rb.TotalWritten() != 1000 {
		t.Errorf("expected TotalWritten=1000, got %d", rb.TotalWritten())
	}
}
