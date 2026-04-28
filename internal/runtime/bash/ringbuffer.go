package bash

import (
	"sync"
)

// RingBuffer is a thread-safe circular buffer that keeps the last maxSize
// bytes written to it. It also tracks total bytes written and a read position
// for incremental output retrieval.
type RingBuffer struct {
	mu       sync.Mutex
	buf      []byte
	maxSize  int
	written  int64 // total bytes written (monotonically increasing)
	readPos  int64 // position of last incremental read
	overflow bool  // true if buffer has wrapped
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(maxSize int) *RingBuffer {
	return &RingBuffer{
		buf:     make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

// Write implements io.Writer. Always returns len(p), nil.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)
	rb.written += int64(n)

	if n >= rb.maxSize {
		// Input larger than buffer — keep only the tail.
		rb.buf = append(rb.buf[:0], p[n-rb.maxSize:]...)
		rb.overflow = true
		return n, nil
	}

	rb.buf = append(rb.buf, p...)
	if len(rb.buf) > rb.maxSize {
		// Trim head to stay within maxSize.
		excess := len(rb.buf) - rb.maxSize
		rb.buf = rb.buf[excess:]
		rb.overflow = true
	}

	return n, nil
}

// Bytes returns all buffered content.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]byte, len(rb.buf))
	copy(out, rb.buf)
	return out
}

// String returns the buffered content as a string.
func (rb *RingBuffer) String() string {
	return string(rb.Bytes())
}

// Incremental returns content written since the last call to Incremental.
// On the first call, returns all buffered content.
// If the buffer has overflowed since the last read, returns all current content
// with a prefix indicating data was lost.
func (rb *RingBuffer) Incremental() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.readPos == 0 {
		// First read — return everything.
		rb.readPos = rb.written
		return string(rb.buf)
	}

	bytesWrittenSinceRead := rb.written - rb.readPos
	rb.readPos = rb.written

	if bytesWrittenSinceRead == 0 {
		return ""
	}

	if bytesWrittenSinceRead > int64(len(rb.buf)) {
		// Buffer overflowed since last read — some data lost.
		return "[... earlier output truncated ...]\n" + string(rb.buf)
	}

	// Return the last N bytes that were written since the previous read.
	start := int64(len(rb.buf)) - bytesWrittenSinceRead
	return string(rb.buf[start:])
}

// TotalWritten returns the total number of bytes written.
func (rb *RingBuffer) TotalWritten() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.written
}

// Len returns the current buffer length.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.buf)
}
