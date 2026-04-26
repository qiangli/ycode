package memos

import (
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var tagPattern = regexp.MustCompile(`(?:^|\s)#([\w][\w-]*)`)

// extractTags finds all #hashtag patterns in markdown content.
func extractTags(content string) []string {
	matches := tagPattern.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool, len(matches))
	tags := make([]string, 0, len(matches))
	for _, m := range matches {
		tag := strings.ToLower(m[1])
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

// computeProperty analyzes memo content for computed properties.
func computeProperty(content string) MemoProperty {
	p := MemoProperty{}
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Title: first heading.
		if p.Title == "" && strings.HasPrefix(trimmed, "# ") {
			p.Title = strings.TrimPrefix(trimmed, "# ")
		}

		// Links.
		if !p.HasLink && (strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://")) {
			p.HasLink = true
		}

		// Code blocks.
		if !p.HasCode && strings.HasPrefix(trimmed, "```") {
			p.HasCode = true
		}

		// Task lists.
		if strings.HasPrefix(trimmed, "- [ ]") || strings.HasPrefix(trimmed, "- [x]") ||
			strings.HasPrefix(trimmed, "* [ ]") || strings.HasPrefix(trimmed, "* [x]") {
			p.HasTaskList = true
			if strings.HasPrefix(trimmed, "- [ ]") || strings.HasPrefix(trimmed, "* [ ]") {
				p.HasIncompleteTasks = true
			}
		}
	}

	return p
}

// generateSnippet creates a plain-text excerpt from markdown content.
func generateSnippet(content string, maxLen int) string {
	// Strip markdown formatting for a clean snippet.
	s := content
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	// Truncate at word boundary.
	if i := strings.LastIndex(s[:maxLen], " "); i > maxLen/2 {
		return s[:i] + "..."
	}
	return s[:maxLen] + "..."
}

// newID generates a unique memo ID.
func newID() string {
	return uuid.New().String()
}

// encodeCursor encodes a pagination cursor.
func encodeCursor(createdAt, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(createdAt + "|" + id))
}

// decodeCursor decodes a pagination cursor into (createdAt, id).
func decodeCursor(token string) (createdAt string, id string, ok bool) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
