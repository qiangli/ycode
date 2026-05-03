package fileops

import (
	"strings"
)

// FuzzyMatch describes a fuzzy match result with the match level and byte offsets.
type FuzzyMatch struct {
	Level     int // 0=exact, 1=line-trimmed, 2=block-anchor
	StartByte int // start offset in original content
	EndByte   int // end offset in original content
}

// FindFuzzyMatch searches for oldString in content using a 4-level fallback:
//   - Level 0: Exact match (strings.Contains)
//   - Level 1: Line-trimmed match (trailing whitespace per line normalized)
//   - Level 2: Indentation-normalized match (matches regardless of absolute indent level)
//   - Level 3: Block-anchor match (first/last line anchors with interior similarity)
//
// Returns nil if no match found at any level.
func FindFuzzyMatch(content, oldString string) *FuzzyMatch {
	// Level 0: Exact match.
	if idx := strings.Index(content, oldString); idx >= 0 {
		return &FuzzyMatch{Level: 0, StartByte: idx, EndByte: idx + len(oldString)}
	}

	// Level 1: Line-trimmed match.
	if m := lineTrimmedMatch(content, oldString); m != nil {
		return m
	}

	// Level 2: Indentation-normalized match (inspired by Aider's RelativeIndenter).
	if m := indentNormalizedMatch(content, oldString); m != nil {
		return m
	}

	// Level 3: Block-anchor match.
	if m := blockAnchorMatch(content, oldString); m != nil {
		return m
	}

	return nil
}

// lineTrimmedMatch normalizes both content and search by trimming trailing whitespace
// per line, then searches for a match. On success, maps back to original byte offsets.
func lineTrimmedMatch(content, search string) *FuzzyMatch {
	normalizedContent := trimTrailingPerLine(content)
	normalizedSearch := trimTrailingPerLine(search)

	if normalizedSearch == "" {
		return nil
	}

	idx := strings.Index(normalizedContent, normalizedSearch)
	if idx < 0 {
		return nil
	}

	// Map normalized byte offset back to original content.
	// Count how many characters we're into the normalized version,
	// then walk the original content line by line to find the same position.
	startByte := mapNormalizedOffset(content, normalizedContent, idx)
	endByte := mapNormalizedOffset(content, normalizedContent, idx+len(normalizedSearch))

	return &FuzzyMatch{Level: 1, StartByte: startByte, EndByte: endByte}
}

// blockAnchorMatch uses the first and last non-empty lines of the search string
// as anchors, then looks for a matching region in the content with similar line count.
func blockAnchorMatch(content, search string) *FuzzyMatch {
	searchLines := strings.Split(search, "\n")
	contentLines := strings.Split(content, "\n")

	// Find first and last non-empty lines of search.
	firstAnchor, lastAnchor := "", ""
	firstIdx, lastIdx := -1, -1
	for i, line := range searchLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if firstIdx == -1 {
				firstIdx = i
				firstAnchor = trimmed
			}
			lastIdx = i
			lastAnchor = trimmed
		}
	}

	if firstIdx == -1 || firstIdx == lastIdx {
		return nil // need at least 2 non-empty lines
	}

	expectedSpan := lastIdx - firstIdx + 1

	// Search content for matching anchors with similar span.
	for i := 0; i < len(contentLines); i++ {
		if strings.TrimSpace(contentLines[i]) != firstAnchor {
			continue
		}

		// Found first anchor at line i. Look for last anchor.
		endLine := i + expectedSpan - 1
		if endLine >= len(contentLines) {
			continue
		}

		if strings.TrimSpace(contentLines[endLine]) != lastAnchor {
			continue
		}

		// Anchors match. Verify interior similarity.
		interiorSim := computeInteriorSimilarity(
			searchLines[firstIdx:lastIdx+1],
			contentLines[i:endLine+1],
		)
		if interiorSim < 0.7 {
			continue
		}

		// Compute byte offsets for the matched region.
		startByte := lineStartOffset(content, i)
		endByte := lineEndOffset(content, endLine)

		return &FuzzyMatch{Level: 3, StartByte: startByte, EndByte: endByte}
	}

	return nil
}

// trimTrailingPerLine trims trailing whitespace from each line.
func trimTrailingPerLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// mapNormalizedOffset maps a byte offset in the normalized string back to the
// corresponding offset in the original string.
func mapNormalizedOffset(original, normalized string, normOffset int) int {
	origLines := strings.Split(original, "\n")
	normLines := strings.Split(normalized, "\n")

	// Walk through normalized lines, accumulating bytes until we reach normOffset.
	normPos := 0
	origPos := 0

	for i := 0; i < len(normLines) && i < len(origLines); i++ {
		if i > 0 {
			normPos++ // newline
			origPos++ // newline
		}

		if normPos+len(normLines[i]) >= normOffset {
			// The offset falls within this line.
			lineOffset := normOffset - normPos
			if lineOffset > len(origLines[i]) {
				lineOffset = len(origLines[i])
			}
			return origPos + lineOffset
		}

		normPos += len(normLines[i])
		origPos += len(origLines[i])
	}

	return origPos
}

// computeInteriorSimilarity compares two line slices and returns 0.0-1.0 similarity.
func computeInteriorSimilarity(search, content []string) float64 {
	if len(search) != len(content) {
		return 0
	}
	if len(search) == 0 {
		return 1.0
	}

	matching := 0
	total := 0
	for i := range search {
		s := strings.TrimSpace(search[i])
		c := strings.TrimSpace(content[i])
		total += max(len(s), len(c))
		matching += commonPrefixLen(s, c)
	}

	if total == 0 {
		return 1.0
	}
	return float64(matching) / float64(total)
}

// commonPrefixLen returns the length of the common prefix between two strings.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// lineStartOffset returns the byte offset of the start of line N in content.
func lineStartOffset(content string, lineNum int) int {
	offset := 0
	for i := 0; i < lineNum; i++ {
		idx := strings.Index(content[offset:], "\n")
		if idx < 0 {
			return offset
		}
		offset += idx + 1
	}
	return offset
}

// lineEndOffset returns the byte offset of the end of line N in content.
func lineEndOffset(content string, lineNum int) int {
	start := lineStartOffset(content, lineNum)
	idx := strings.Index(content[start:], "\n")
	if idx < 0 {
		return len(content)
	}
	return start + idx
}

// indentNormalizedMatch handles the common case where the LLM produces correct
// code but at the wrong indentation level. It strips the base indentation from
// both the search text and candidate regions in the content, then compares
// the "de-indented" versions. Inspired by Aider's RelativeIndenter.
func indentNormalizedMatch(content, search string) *FuzzyMatch {
	searchLines := strings.Split(search, "\n")
	contentLines := strings.Split(content, "\n")

	// Need at least 2 non-empty lines to match meaningfully.
	nonEmpty := 0
	for _, l := range searchLines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty < 2 {
		return nil
	}

	// Compute base indent of search (minimum indent among non-empty lines).
	searchBase := baseIndent(searchLines)

	// De-indent the search text.
	deindentedSearch := deindentLines(searchLines, searchBase)

	// Slide a window of len(searchLines) over the content.
	windowSize := len(searchLines)
	if windowSize > len(contentLines) {
		return nil
	}

	for i := 0; i <= len(contentLines)-windowSize; i++ {
		window := contentLines[i : i+windowSize]

		// Compute base indent of this window.
		windowBase := baseIndent(window)

		// De-indent the window.
		deindentedWindow := deindentLines(window, windowBase)

		// Compare de-indented versions.
		if slicesEqual(deindentedSearch, deindentedWindow) {
			startByte := lineStartOffset(content, i)
			endByte := lineEndOffset(content, i+windowSize-1)
			return &FuzzyMatch{Level: 2, StartByte: startByte, EndByte: endByte}
		}
	}

	return nil
}

// baseIndent returns the minimum leading whitespace count among non-empty lines.
func baseIndent(lines []string) int {
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingSpaces(line)
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent < 0 {
		return 0
	}
	return minIndent
}

// leadingSpaces counts leading space characters (tabs count as 1).
func leadingSpaces(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			count++
		} else {
			break
		}
	}
	return count
}

// deindentLines removes baseN leading characters from each non-empty line.
func deindentLines(lines []string, baseN int) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			result[i] = ""
		} else if len(line) >= baseN {
			result[i] = line[baseN:]
		} else {
			result[i] = line
		}
	}
	return result
}

// slicesEqual compares two string slices for equality.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
