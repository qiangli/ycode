package bash

import (
	"fmt"
	"strings"
)

// TruncateOutput limits output to maxSize bytes, preserving both the beginning
// and end of the output. This is critical for command output where errors and
// results cluster at the end (compiler errors, test failures, exit messages)
// while the beginning provides context (command headers, initial output).
//
// The split uses a 30/70 head/tail ratio: 30% head, 70% tail. This bias
// toward the tail reflects real-world command output patterns where actionable
// information (errors, results) appears at the end.
func TruncateOutput(s string, maxSize int) string {
	if len(s) <= maxSize {
		return strings.TrimRight(s, "\n")
	}

	// Reserve space for the omission marker.
	omitted := len(s) - maxSize
	marker := fmt.Sprintf("\n\n[... %d bytes omitted ...]\n\n", omitted)
	available := maxSize - len(marker)
	if available <= 0 {
		// Degenerate case: maxSize too small for even a marker.
		return s[:maxSize]
	}

	headSize := available * 3 / 10 // 30% head
	tailSize := available - headSize

	head := s[:headSize]
	tail := s[len(s)-tailSize:]

	return strings.TrimRight(head, "\n") + marker + strings.TrimLeft(tail, "\n")
}
