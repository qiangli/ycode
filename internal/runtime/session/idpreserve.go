package session

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// IdentifierPolicy controls how identifiers are preserved during compaction.
type IdentifierPolicy int

const (
	// IDPreserveStrict extracts and injects all detected identifiers.
	IDPreserveStrict IdentifierPolicy = iota
	// IDPreserveCustom uses a caller-supplied filter function.
	IDPreserveCustom
	// IDPreserveOff disables identifier preservation.
	IDPreserveOff
)

// Identifier represents a detected identifier with its type.
type Identifier struct {
	Value string
	Kind  string // "file_path", "git_hash", "uuid", "url", "go_package"
}

var (
	// File paths: absolute or relative, with extension.
	reFilePath = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])(/[a-zA-Z0-9_./-]{2,100}\.[a-zA-Z0-9]{1,10}|\.{1,2}/[a-zA-Z0-9_./-]{2,100}\.[a-zA-Z0-9]{1,10})`)
	// Git hashes: 7-40 hex chars, preceded by whitespace or start of line.
	reGitHash = regexp.MustCompile(`(?:^|[\s(])([0-9a-f]{7,40})(?:[\s),.]|$)`)
	// UUIDs: standard 8-4-4-4-12.
	reUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// Go package paths.
	reGoPackage = regexp.MustCompile(`(?:^|[\s"'` + "`" + `])([a-zA-Z0-9]+\.[a-zA-Z0-9]+/[a-zA-Z0-9_./-]+)`)
)

// ExtractIdentifiers scans text for file paths, git hashes, UUIDs, URLs,
// and Go package paths. Returns deduplicated identifiers.
func ExtractIdentifiers(text string) []Identifier {
	seen := make(map[string]bool)
	var ids []Identifier

	add := func(value, kind string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		ids = append(ids, Identifier{Value: value, Kind: kind})
	}

	for _, m := range reFilePath.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			add(m[1], "file_path")
		}
	}

	for _, m := range reGitHash.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			// Filter out common false positives (too short, all zeros).
			h := m[1]
			if len(h) >= 7 && h != strings.Repeat("0", len(h)) {
				add(h, "git_hash")
			}
		}
	}

	for _, m := range reUUID.FindAllString(text, -1) {
		add(m, "uuid")
	}

	for _, m := range reGoPackage.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			add(m[1], "go_package")
		}
	}

	return ids
}

// FormatPreservationInstruction creates a compaction instruction block that
// tells the LLM to preserve specific identifiers in the summary.
// Returns empty string if no identifiers found or policy is off.
func FormatPreservationInstruction(identifiers []Identifier, policy IdentifierPolicy) string {
	if policy == IDPreserveOff || len(identifiers) == 0 {
		return ""
	}

	// Group by kind for readability.
	grouped := make(map[string][]string)
	for _, id := range identifiers {
		grouped[id.Kind] = append(grouped[id.Kind], id.Value)
	}

	// Sort kinds for deterministic output (important for prompt caching).
	kinds := make([]string, 0, len(grouped))
	for k := range grouped {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	var sb strings.Builder
	sb.WriteString("IMPORTANT: The following identifiers MUST be preserved exactly in the summary:\n")

	for _, kind := range kinds {
		values := grouped[kind]
		// Limit to 20 per kind to avoid bloating the instruction.
		if len(values) > 20 {
			values = values[:20]
		}
		fmt.Fprintf(&sb, "  %s: %s\n", kind, strings.Join(values, ", "))
	}

	sb.WriteString("Do not paraphrase, abbreviate, or omit these identifiers.\n")
	return sb.String()
}

// ExtractFromMessages extracts identifiers from a slice of conversation messages.
func ExtractFromMessages(messages []ConversationMessage) []Identifier {
	var allText strings.Builder
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == ContentTypeText {
				allText.WriteString(block.Text)
				allText.WriteString("\n")
			}
		}
	}
	return ExtractIdentifiers(allText.String())
}
