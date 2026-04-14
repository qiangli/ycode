package redact

import (
	"regexp"
	"strings"
)

// Redactor replaces sensitive data patterns with placeholders.
type Redactor struct {
	patterns []compiledPattern
}

type compiledPattern struct {
	re          *regexp.Regexp
	replacement string
}

// DefaultPatterns returns a Redactor with standard patterns for API keys,
// tokens, secrets, and PII.
func DefaultPatterns() *Redactor {
	return New([]Pattern{
		// API keys — common prefixes.
		{Name: "anthropic_key", Regex: `\bsk-ant-[A-Za-z0-9_-]{20,}\b`, Replacement: "[REDACTED:anthropic_key]"},
		{Name: "openai_key", Regex: `\bsk-[A-Za-z0-9]{20,}\b`, Replacement: "[REDACTED:openai_key]"},
		{Name: "bearer_token", Regex: `(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{20,}\b`, Replacement: "Bearer [REDACTED]"},

		// Generic secret patterns.
		{Name: "api_key_param", Regex: `(?i)(api[_-]?key|apikey|secret[_-]?key|access[_-]?token|auth[_-]?token)\s*[=:]\s*["']?[A-Za-z0-9._~+/=-]{8,}["']?`, Replacement: "${1}=[REDACTED]"},

		// AWS credentials.
		{Name: "aws_access_key", Regex: `\bAKIA[0-9A-Z]{16}\b`, Replacement: "[REDACTED:aws_key]"},
		{Name: "aws_secret_key", Regex: `(?i)(aws_secret_access_key|aws_secret)\s*[=:]\s*["']?[A-Za-z0-9/+=]{30,}["']?`, Replacement: "${1}=[REDACTED]"},

		// GitHub tokens.
		{Name: "github_token", Regex: `\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`, Replacement: "[REDACTED:github_token]"},

		// Generic long hex/base64 strings that look like secrets (40+ chars).
		{Name: "long_hex_secret", Regex: `(?i)(password|passwd|secret|token|credential)\s*[=:]\s*["']?[A-Fa-f0-9]{40,}["']?`, Replacement: "${1}=[REDACTED]"},

		// Email addresses (basic PII).
		{Name: "email", Regex: `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, Replacement: "[REDACTED:email]"},
	})
}

// Pattern defines a named redaction rule.
type Pattern struct {
	Name        string
	Regex       string
	Replacement string
}

// New creates a Redactor from the given patterns.
// Invalid regexes are silently skipped.
func New(patterns []Pattern) *Redactor {
	r := &Redactor{}
	for _, p := range patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			continue
		}
		r.patterns = append(r.patterns, compiledPattern{
			re:          re,
			replacement: p.Replacement,
		})
	}
	return r
}

// Redact replaces all sensitive patterns in s.
func (r *Redactor) Redact(s string) string {
	for _, p := range r.patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	return s
}

// RedactMap redacts values in a map (shallow — string values only).
func (r *Redactor) RedactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			out[k] = r.Redact(val)
		default:
			_ = val
			out[k] = v
		}
	}
	return out
}

// ContainsSensitive returns true if the string matches any pattern.
func (r *Redactor) ContainsSensitive(s string) bool {
	for _, p := range r.patterns {
		if p.re.MatchString(s) {
			return true
		}
	}
	return false
}

// RedactEnvStyle redacts values in KEY=VALUE formatted strings.
// Useful for sanitizing environment variable dumps.
func (r *Redactor) RedactEnvStyle(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = r.Redact(line)
	}
	return strings.Join(lines, "\n")
}
