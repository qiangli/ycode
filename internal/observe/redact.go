package observe

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// redactPlaceholder replaces any matched secret material.
const redactPlaceholder = "[REDACTED]"

// secretPatterns scrubs credentials before any free-text field is written.
// The invariant is non-negotiable: vault secrets and API keys MUST NEVER appear
// in the action log. Order matters — keyed-value patterns run first so a
// `"api_key": "sk-..."` is masked as a whole, then vendor-prefix patterns catch
// any bare token that slipped through.
var secretPatterns = []*regexp.Regexp{
	// key/value assignments: api_key, apikey, secret, token, password, access_key,
	// authorization — in JSON ("k": "v"), env (K=V), or query (k=v) shapes.
	regexp.MustCompile(`(?i)("?(?:api[_-]?key|apikey|secret|secret[_-]?key|access[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|passwd|pwd|token|authorization|auth[_-]?token|bearer)"?\s*[:=]\s*"?)([A-Za-z0-9._\-+/=~]{6,})`),
	// Authorization: Bearer <token>
	regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9._\-+/=~]{8,})`),
	// Vendor-prefixed keys (Anthropic, OpenAI, GitHub, Slack, AWS, Google).
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{20,}`),
	// Generic high-entropy long hex/base64 secrets are intentionally NOT matched
	// wholesale to avoid mangling legitimate output (hashes, IDs); the keyed and
	// vendor patterns above cover the credential-shaped cases.
}

// Redact removes credentials from s. It preserves the key name in a key/value
// pair (so the log still shows THAT an api key was present) while masking the
// value.
func Redact(s string) string {
	if s == "" {
		return s
	}
	// The first two patterns capture the key/prefix in group 1 and the secret
	// value in group 2 — keep group 1, mask group 2.
	for _, re := range secretPatterns[:2] {
		s = re.ReplaceAllString(s, "${1}"+redactPlaceholder)
	}
	for _, re := range secretPatterns[2:] {
		s = re.ReplaceAllString(s, redactPlaceholder)
	}
	return s
}

// hashRef returns a short, stable reference to s that never reveals its
// contents: "sha256:" + the first 16 hex chars of the digest. Used for prompt
// and completion references so records are comparable without dumping text.
func hashRef(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// truncate bounds a free-text field to max runes, appending an elision marker
// with the original length so the reader knows it was clipped. max <= 0 means
// no limit (verbose mode).
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	// Trim on a rune boundary to keep valid UTF-8.
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…[truncated]"
}
