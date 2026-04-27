package security

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// InjectionFinding represents a detected injection pattern.
type InjectionFinding struct {
	Pattern  string   // name of the matched pattern
	Severity Severity // Low, Medium, High
	Location string   // description of where found
	Snippet  string   // short excerpt showing the match
}

// Severity indicates how dangerous a detected injection pattern is.
type Severity int

const (
	SeverityLow    Severity = iota // suspicious but may be benign
	SeverityMedium                 // likely intentional injection
	SeverityHigh                   // known injection technique
)

// String returns the human-readable severity label.
func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "Low"
	case SeverityMedium:
		return "Medium"
	case SeverityHigh:
		return "High"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// maxSnippetLen is the maximum length of a snippet in a finding.
const maxSnippetLen = 80

// ScanForInjection checks content for prompt injection patterns.
// Returns nil if no issues found.
func ScanForInjection(content string) []InjectionFinding {
	var findings []InjectionFinding

	findings = append(findings, scanKnownPhrases(content)...)
	findings = append(findings, scanInvisibleUnicode(content)...)
	findings = append(findings, scanHTMLTags(content)...)
	findings = append(findings, scanBase64Blocks(content)...)

	if len(findings) == 0 {
		return nil
	}
	return findings
}

// Known injection phrases (case-insensitive).
var knownPhrases = []string{
	`ignore previous instructions`,
	`ignore all previous`,
	`disregard previous`,
	`forget your instructions`,
	`you are now`,
	`act as if`,
	`pretend you are`,
	`new system prompt`,
	`override your`,
}

var knownPhrasePatterns []*regexp.Regexp

func init() {
	for _, phrase := range knownPhrases {
		// Build case-insensitive pattern that matches across word boundaries.
		knownPhrasePatterns = append(knownPhrasePatterns,
			regexp.MustCompile(`(?i)`+regexp.QuoteMeta(phrase)))
	}
}

func scanKnownPhrases(content string) []InjectionFinding {
	var findings []InjectionFinding
	for _, re := range knownPhrasePatterns {
		loc := re.FindStringIndex(content)
		if loc != nil {
			findings = append(findings, InjectionFinding{
				Pattern:  "known_injection_phrase",
				Severity: SeverityHigh,
				Location: fmt.Sprintf("offset %d", loc[0]),
				Snippet:  snippet(content, loc[0], loc[1]),
			})
		}
	}
	return findings
}

// Invisible unicode characters that can hide content.
var invisibleRunes = map[rune]string{
	'\u200B': "zero-width space",
	'\u200C': "zero-width non-joiner",
	'\u200D': "zero-width joiner",
	'\u202D': "left-to-right override",
	'\u202E': "right-to-left override",
	'\u2060': "word joiner",
}

// maxInvisibleFindings caps the number of invisible unicode findings to avoid
// flooding the results when a document contains many zero-width characters.
const maxInvisibleFindings = 10

func scanInvisibleUnicode(content string) []InjectionFinding {
	var findings []InjectionFinding
	for i, r := range content {
		if name, ok := invisibleRunes[r]; ok {
			findings = append(findings, InjectionFinding{
				Pattern:  "invisible_unicode",
				Severity: SeverityHigh,
				Location: fmt.Sprintf("offset %d", i),
				Snippet:  fmt.Sprintf("U+%04X (%s)", r, name),
			})
			if len(findings) >= maxInvisibleFindings {
				break
			}
		}
	}
	return findings
}

// HTML/script tag patterns (case-insensitive).
var htmlPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<script`),
	regexp.MustCompile(`(?i)<iframe`),
	regexp.MustCompile(`(?i)<div\s+style\s*=\s*"display:\s*none"`),
	regexp.MustCompile(`(?i)<div\s+style\s*=\s*"visibility:\s*hidden"`),
	regexp.MustCompile(`<!--`),
}

var htmlPatternNames = []string{
	"script_tag",
	"iframe_tag",
	"hidden_div_display",
	"hidden_div_visibility",
	"html_comment",
}

func scanHTMLTags(content string) []InjectionFinding {
	var findings []InjectionFinding
	for i, re := range htmlPatterns {
		loc := re.FindStringIndex(content)
		if loc != nil {
			findings = append(findings, InjectionFinding{
				Pattern:  htmlPatternNames[i],
				Severity: SeverityMedium,
				Location: fmt.Sprintf("offset %d", loc[0]),
				Snippet:  snippet(content, loc[0], loc[1]),
			})
		}
	}
	return findings
}

// Base64 pattern: long runs of base64 characters (>100).
var base64Pattern = regexp.MustCompile(`[A-Za-z0-9+/=]{100,}`)

func scanBase64Blocks(content string) []InjectionFinding {
	var findings []InjectionFinding
	locs := base64Pattern.FindAllStringIndex(content, -1)
	for _, loc := range locs {
		findings = append(findings, InjectionFinding{
			Pattern:  "base64_block",
			Severity: SeverityLow,
			Location: fmt.Sprintf("offset %d, length %d", loc[0], loc[1]-loc[0]),
			Snippet:  snippet(content, loc[0], loc[1]),
		})
	}
	return findings
}

// snippet extracts a short excerpt from content around [start, end).
func snippet(content string, start, end int) string {
	// Expand a little for context.
	s := max(start-10, 0)
	e := min(end+10, len(content))
	snip := content[s:e]
	if len(snip) > maxSnippetLen {
		snip = snip[:maxSnippetLen]
	}
	// Ensure we don't split a multi-byte UTF-8 character at the end.
	for len(snip) > 0 && !utf8.ValidString(snip) {
		snip = snip[:len(snip)-1]
	}
	// Replace control chars for readability.
	snip = strings.ReplaceAll(snip, "\n", "\\n")
	snip = strings.ReplaceAll(snip, "\r", "\\r")
	snip = strings.ReplaceAll(snip, "\t", "\\t")
	return snip
}
