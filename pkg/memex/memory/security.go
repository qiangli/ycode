package memory

import (
	"regexp"
	"strings"
)

// SecurityFinding describes a potential security issue found in memory content.
type SecurityFinding struct {
	Type    string `json:"type"`    // injection, secret, unicode
	Pattern string `json:"pattern"` // the matched pattern
	Excerpt string `json:"excerpt"` // the matching text (truncated)
}

// ContentScanner scans memory content for security issues.
type ContentScanner struct {
	injectionPatterns []*regexp.Regexp
	secretPatterns    []*regexp.Regexp
}

// NewContentScanner creates a content scanner with default patterns.
func NewContentScanner() *ContentScanner {
	return &ContentScanner{
		injectionPatterns: compilePatterns([]string{
			`(?i)ignore\s+(all\s+)?previous\s+instructions`,
			`(?i)ignore\s+(all\s+)?above\s+instructions`,
			`(?i)disregard\s+(all\s+)?previous`,
			`(?i)you\s+are\s+now\s+a`,
			`(?i)pretend\s+you\s+are`,
			`(?i)act\s+as\s+if\s+you`,
			`(?i)system\s*:\s*you\s+are`,
			`(?i)new\s+instructions?\s*:`,
			`(?i)override\s+(your\s+)?instructions`,
		}),
		secretPatterns: compilePatterns([]string{
			`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*\S+`,
			`(?i)(?:secret|token|password|passwd|pwd)\s*[:=]\s*\S+`,
			`sk-[a-zA-Z0-9]{20,}`,             // OpenAI-style
			`ghp_[a-zA-Z0-9]{36,}`,            // GitHub PAT
			`gho_[a-zA-Z0-9]{36,}`,            // GitHub OAuth
			`xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+`, // Slack bot
			`AKIA[0-9A-Z]{16}`,                // AWS access key
		}),
	}
}

// Scan checks content for security issues and returns findings.
func (s *ContentScanner) Scan(content string) []SecurityFinding {
	var findings []SecurityFinding

	// Check injection patterns.
	for _, re := range s.injectionPatterns {
		if loc := re.FindStringIndex(content); loc != nil {
			excerpt := safeExcerpt(content, loc[0], loc[1])
			findings = append(findings, SecurityFinding{
				Type:    "injection",
				Pattern: re.String(),
				Excerpt: excerpt,
			})
		}
	}

	// Check secret patterns.
	for _, re := range s.secretPatterns {
		if loc := re.FindStringIndex(content); loc != nil {
			excerpt := safeExcerpt(content, loc[0], loc[1])
			findings = append(findings, SecurityFinding{
				Type:    "secret",
				Pattern: re.String(),
				Excerpt: excerpt,
			})
		}
	}

	// Check for invisible Unicode characters (zero-width).
	if hasInvisibleUnicode(content) {
		findings = append(findings, SecurityFinding{
			Type:    "unicode",
			Pattern: "invisible unicode characters",
			Excerpt: "content contains zero-width or invisible characters",
		})
	}

	return findings
}

// hasInvisibleUnicode checks for zero-width and other invisible Unicode characters.
func hasInvisibleUnicode(s string) bool {
	for _, r := range s {
		switch r {
		case '\u200B', // zero-width space
			'\u200C', // zero-width non-joiner
			'\u200D', // zero-width joiner
			'\u2060', // word joiner
			'\uFEFF', // BOM / zero-width no-break space
			'\u00AD': // soft hyphen
			return true
		}
	}
	return false
}

// safeExcerpt returns a truncated excerpt of the matched content.
func safeExcerpt(content string, start, end int) string {
	// Extend context by 10 chars each side.
	lo := start - 10
	if lo < 0 {
		lo = 0
	}
	hi := end + 10
	if hi > len(content) {
		hi = len(content)
	}
	excerpt := content[lo:hi]
	// Mask any potential secrets.
	if len(excerpt) > 40 {
		excerpt = excerpt[:20] + "..." + excerpt[len(excerpt)-10:]
	}
	return strings.ReplaceAll(excerpt, "\n", " ")
}

// compilePatterns compiles regex patterns, skipping invalid ones.
func compilePatterns(patterns []string) []*regexp.Regexp {
	var compiled []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}
