package message

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ElideMiddle bounds text to about maxBytes by keeping its head and tail
// halves and replacing the middle with a note that says how much was left
// out. Cuts fall on line boundaries when one is near, and never inside a
// UTF-8 sequence. It reports whether anything was elided; maxBytes <= 0 or
// text that already fits is returned unchanged.
func ElideMiddle(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text, false
	}
	half := maxBytes / 2
	headEnd := half
	if i := strings.LastIndexByte(text[:half], '\n'); i >= half/2 {
		headEnd = i + 1
	}
	for headEnd > 0 && headEnd < len(text) && !utf8.RuneStart(text[headEnd]) {
		headEnd--
	}
	tailStart := len(text) - half
	if i := strings.IndexByte(text[tailStart:], '\n'); i >= 0 && i < half/2 {
		tailStart += i + 1
	}
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	if tailStart < headEnd {
		tailStart = headEnd
	}
	omitted := text[headEnd:tailStart]
	lines := strings.Count(omitted, "\n")
	if omitted != "" && !strings.HasSuffix(omitted, "\n") {
		lines++
	}
	head := strings.TrimSuffix(text[:headEnd], "\n")
	note := fmt.Sprintf("[output truncated: %d lines (%d bytes) omitted]", lines, len(omitted))
	return head + "\n" + note + "\n" + text[tailStart:], true
}
