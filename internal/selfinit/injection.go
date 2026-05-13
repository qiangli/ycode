package selfinit

import (
	"fmt"
	"strings"
)

// Markdown markers used to scope ycode's edits in foreign-tool config
// files (~/.claude.json, ~/.config/opencode/mcp.json). SpliceBlock
// replaces by these literals so re-runs are idempotent.
const (
	BeginMarker = "<!-- BEGIN YCODE -->"
	EndMarker   = "<!-- END YCODE -->"
)

// SpliceBlock returns existing with the YCODE block replaced by block.
// If existing has no YCODE block, the block is appended.
//
// Spacing is normalised on every call so repeated splices converge to
// a fixed point: always one blank line between the user's content and
// the BEGIN marker (if there's user content), and a single trailing
// newline at end-of-file.
//
// block must NOT include the BEGIN/END marker lines — SpliceBlock adds
// them. Just supply the body.
func SpliceBlock(existing, block string) string {
	wrapped := WrapBlock(block)
	if existing == "" {
		return wrapped + "\n"
	}
	var pre, post string
	if start, end, ok := findBlock(existing); ok {
		pre = strings.TrimRight(existing[:start], "\n")
		post = strings.TrimLeft(existing[end:], "\n")
	} else {
		pre = strings.TrimRight(existing, "\n")
	}
	post = strings.TrimRight(post, "\n")

	switch {
	case pre == "" && post == "":
		return wrapped + "\n"
	case pre == "":
		return wrapped + "\n\n" + post + "\n"
	case post == "":
		return pre + "\n\n" + wrapped + "\n"
	default:
		return pre + "\n\n" + wrapped + "\n\n" + post + "\n"
	}
}

// WrapBlock wraps body with the BEGIN/END markers. body should not
// include trailing newlines; WrapBlock controls the separators.
func WrapBlock(body string) string {
	body = strings.TrimRight(body, "\n")
	return fmt.Sprintf("%s\n%s\n%s", BeginMarker, body, EndMarker)
}

// HasBlock reports whether existing contains a YCODE delimited block.
func HasBlock(existing string) bool {
	_, _, ok := findBlock(existing)
	return ok
}

// findBlock returns the byte range covering [BEGIN-line .. END-line]
// inclusive of the trailing newline after END. SpliceBlock trims its
// own whitespace, so we don't need to do clever leading-blank-line
// consumption here.
func findBlock(existing string) (int, int, bool) {
	bIdx := strings.Index(existing, BeginMarker)
	if bIdx < 0 {
		return 0, 0, false
	}
	eIdx := strings.Index(existing[bIdx:], EndMarker)
	if eIdx < 0 {
		return 0, 0, false
	}
	end := bIdx + eIdx + len(EndMarker)
	if end < len(existing) && existing[end] == '\n' {
		end++
	}
	return bIdx, end, true
}
